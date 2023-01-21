package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/google/gousb"
	"github.com/tarm/serial"
)

type Device struct {
	vedorId   string
	productId string
}

func main() {
	// Initialize a new Context.
	ctx := gousb.NewContext()

	devices := []Device{}

	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		//log.Println(strconv.Itoa(desc.Address))
		if desc.SubClass.String() == "communications" {
			devices = append(devices, Device{desc.Vendor.String(), desc.Product.String()})
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

	c := &serial.Config{Name: "/dev/ttyUSB0", Baud: 115200}
	s, err := serial.OpenPort(c)

	if err != nil {
		log.Fatal(err)
	} else {
		file, err := os.OpenFile("/etc/udev/rules.d/49-boxconfig.rules", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

		if err != nil {
			log.Println("Could not open example.txt")
			return
		}
		result := fmt.Sprintf(`KERNEL=="ttyUSB[0-9]*", SUBSYSTEM=="tty", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s", SYMLINK="ttyBOX%s"`, devices[0].vedorId, devices[0].productId, devices[0].productId)
		_, err2 := file.WriteString(result)

		if err2 != nil {
			log.Println("Could not write")

		} else {
			log.Println("Operation successful!")
			cmd := exec.Command("reboot")

			err := cmd.Run()

			if err != nil {
				log.Fatal(err)
			}
		}

	}

	s.Close()

}
