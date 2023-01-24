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
	"strings"
	"time"

	"github.com/google/gousb"
	"github.com/gorilla/websocket"
	"github.com/tarm/serial"
)

type Box struct {
	Vendor  string `json:Vendor`
	Product string `json:Product`
}

type Header struct {
	Type string
}

type BodyList struct {
	Data []Box `json:Data`
}

type BodySwitch struct {
	Data struct {
		Product string
		Vendor  string
		Command string
	}
}

const source = "ttyACM"

func main() {
	// Initialize a new Context.
	ctx := gousb.NewContext()

	devices := []Box{}

	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		//log.Println(strconv.Itoa(desc.Address))
		if desc.SubClass.String() == "communications" {
			devices = append(devices, Box{desc.Vendor.String(), desc.Product.String()})
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
	u := url.URL{Scheme: "ws", Host: "192.168.1.15:3001", Path: "/node/webs", RawQuery: rawQuery}
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
									result := fmt.Sprintf(`KERNEL=="%s[0-9]*", SUBSYSTEM=="tty", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s", SYMLINK="ttyBOX%s"%s`, source, dox.Vendor, dox.Product, dox.Product, "\n")
									_, err2 := file.WriteString(result)

									if err2 != nil {
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
									log.Println(err)
								}
							}

							for _, box := range devices {
								go func(box Box) {
									log.Println("/dev/ttyBOX" + box.Product)
									c := &serial.Config{Name: "/dev/ttyBOX" + box.Product, Baud: 115200}
									s, err := serial.OpenPort(c)

									if err != nil {
										log.Println(err)
										close(serialChannels[box.Vendor+box.Product])
									} else {
										serialChannels[box.Vendor+box.Product] = make(chan string, 10)
										defer close(serialChannels[box.Vendor+box.Product])

										for {
											mess := <-serialChannels[box.Vendor+box.Product]
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
							serialChannels[swit.Data.Vendor+swit.Data.Product] <- swit.Data.Command
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
			log.Println(s1Val.Vendor + s1Val.Product + "  --  " + s2Val.Vendor + s2Val.Product)
			if s1Val.Vendor+s1Val.Product == s2Val.Vendor+s2Val.Product {
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
