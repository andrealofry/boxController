package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/google/gousb"
	"github.com/gorilla/websocket"
	"github.com/tarm/serial"
)

type Box struct {
	Path string `json:Path`
}

type Header struct {
	Type string
}

type BodyList struct {
	Data []Box `json:Data`
}

type BodySwitch struct {
	Data struct {
		Path    string
		Command string
	}
}

func main() {
	// Initialize a new Context.
	ctx := gousb.NewContext()

	devices := []Box{}

	/*cmd := exec.Command("chmod 666 /dev/ttyUSB0")
	errC := cmd.Run()
	if errC != nil {
		log.Println(errC)
	}*/

	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		log.Println(desc.SubClass.String(), desc.Protocol.String(), desc.Class.String(), desc.Address, desc.Bus, desc.Configs, desc.Device.String(), desc.MaxControlPacketSize, desc.Path, desc.Port, desc.Product.String(), desc.Spec.String(), desc.Speed.String(), desc.Vendor.String())

		if desc.Vendor.String() == "0403" && desc.Product.String() == "6001" {
			devices = append(devices, Box{strconv.Itoa(desc.Bus) + "-" + strings.Trim(strings.Join(strings.Fields(fmt.Sprint(desc.Path)), "."), "[]")})
		}
		if desc.Vendor.String() == "04e2" && desc.Product.String() == "1410" {
			devices = append(devices, Box{strconv.Itoa(desc.Bus) + "-" + strings.Trim(strings.Join(strings.Fields(fmt.Sprint(desc.Path)), "."), "[]")})
		}
		return false
	})

	if err != nil {
		log.Fatalf("Could not open a device: %v", err)
	}
	for _, dev := range devs {
		dev.Close()
	}

	ctx.Close()

	/*c := &serial.Config{Name: "/dev/ttyUSB0", Baud: 115200}
	s, err := serial.OpenPort(c)

	if err != nil {
		log.Println(err)
	} else {

		s.Close()
	}*/

	messageOut := make(chan string)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	rawQuery := "id=" + getMac()
	u := url.URL{Scheme: "wss", Host: "new.jkssrl.com", Path: "/node/webs", RawQuery: rawQuery}
	log.Printf("connecting to %s", u.String())

	ws, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("handshake failed with status %d", resp.StatusCode)
		log.Println("dial:", err)
	}
	defer ws.Close()

	var serialChannels = make(map[string](chan string))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
			go func() {
				var header Header
				err2 := json.Unmarshal(message, &header)

				if err2 == nil {
					switch Type := header.Type; Type {
					case "list":
						var list BodyList
						err3 := json.Unmarshal(message, &list)
						if err3 == nil {

							doxa := difference(devices, list.Data)
							log.Println(doxa)
							if len(doxa) > 0 {
								file, err := os.OpenFile("/etc/udev/rules.d/49-boxconfig.rules", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
								if err != nil {
									log.Println("Could not open example.txt")
									return
								}

								for _, dox := range doxa {
									//result := fmt.Sprintf(`KERNEL=="%s[0-9]*", SUBSYSTEM=="tty", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s", SYMLINK="ttyBOX%s", MODE="0666", GROUP="root"%s`, source, dox.Vendor, dox.Product, dox.Product, "\n")
									result := fmt.Sprintf(`KERNEL=="ttyUSB*", KERNELS=="%s", SYMLINK+="ttyBOX%s" MODE="0666", GROUP="root"%s`, dox.Path, dox.Path, "\n")

									_, err2 := file.WriteString(result)

									if err2 != nil {
										log.Println("Could not write")

									} else {
										log.Println("Operation successful!")
									}
									result2 := fmt.Sprintf(`KERNEL=="ttyACM*", KERNELS=="%s", SYMLINK+="ttyBOX%s" MODE="0666", GROUP="root"%s`, dox.Path, dox.Path, "\n")
									_, err3 := file.WriteString(result2)

									if err3 != nil {
										log.Println("Could not write")

									} else {
										log.Println("Operation successful!")
									}
								}
								b, errM := json.Marshal(struct {
									Type string `json:"type"`
									Data []Box  `json:"data"`
								}{"list", doxa})

								if errM != nil {
									log.Println("Could not json stringify")
									return
								}
								log.Println(b)
								messageOut <- string(b)

								cmd := exec.Command("reboot")
								errC := cmd.Run()
								if errC != nil {
									log.Println(errC)
								}
							}

							for _, box := range devices {
								go func(box Box) {
									log.Println("/dev/ttyBOX" + box.Path)
									c := &serial.Config{Name: "/dev/ttyBOX" + box.Path, Baud: 115200}
									s, err := serial.OpenPort(c)

									if err != nil {
										log.Println(err)
										close(serialChannels[box.Path])
									} else {
										serialChannels[box.Path] = make(chan string, 10)
										defer close(serialChannels[box.Path])

										for {
											mess := <-serialChannels[box.Path]
											log.Printf("message receive: %s\n", mess)

											time.Sleep(time.Millisecond * 750)
											_, errr := s.Write([]byte(mess + "\r\n"))
											if errr != nil {
												log.Println(errr)
											}

										}

									}
									defer s.Close()

								}(box)
							}
						}
					case "switch":
						var swit BodySwitch
						err3 := json.Unmarshal(message, &swit)
						if err3 != nil {
							log.Println(err3)
						} else {
							serialChannels[swit.Data.Path] <- swit.Data.Command
						}
					}
				}
			}()
		}

	}()

	/*ticker := time.NewTicker(time.Second)
	defer ticker.Stop()*/
	for {
		select {
		case <-done:
			return
		case m := <-messageOut:
			log.Printf("Send Message %s", m)
			err := ws.WriteMessage(websocket.TextMessage, []byte(m))
			if err != nil {
				log.Println("write:", err)
				return
			}
		/*case t := <-ticker.C:
		err := ws.WriteMessage(websocket.TextMessage, []byte(t.String()))
		if err != nil {
			log.Println("write:", err)
			return
		}*/
		case <-interrupt:
			log.Println("interrupt")
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func difference(slice1 []Box, slice2 []Box) []Box {
	diffStr := []Box{}

	for _, s1Val := range slice1 {
		var condition = true
		for _, s2Val := range slice2 {
			log.Println(s1Val.Path + "  --  " + s2Val.Path)
			if s1Val.Path == s2Val.Path {
				condition = false
				break
			}
		}
		if condition {
			diffStr = append(diffStr, s1Val)
		}
	}

	return diffStr
}

func getMac() string {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		fmt.Println(err)
	}

	var currentIP, currentNetworkHardwareName string

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				currentIP = ipnet.IP.String()
			}
		}
	}

	interfaces, _ := net.Interfaces()
	for _, interf := range interfaces {

		if addrs, err := interf.Addrs(); err == nil {
			for _, addr := range addrs {
				if strings.Contains(addr.String(), currentIP) {
					currentNetworkHardwareName = interf.Name
				}
			}
		}
	}

	netInterface, err := net.InterfaceByName(currentNetworkHardwareName)

	if err != nil {
		fmt.Println(err)
	}

	macAddress := netInterface.HardwareAddr

	fmt.Println("MAC address : ", macAddress)

	// verify if the MAC address can be parsed properly
	hwAddr, err := net.ParseMAC(macAddress.String())

	if err != nil {
		fmt.Println("No able to parse MAC address : ", err)
		os.Exit(-1)
	}

	return hwAddr.String()
}
