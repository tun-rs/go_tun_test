package normal_tun

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/songgao/water"
)

const (
	IFACE1_NAME = "tun11"
	IFACE1_IP   = "10.0.1.1"
	IFACE1_CIDR = "24"
	IFACE1_NET  = "10.0.1.0/24"

	IFACE2_NAME = "tun22"
	IFACE2_IP   = "10.0.2.1"
	IFACE2_CIDR = "24"
	IFACE2_NET  = "10.0.2.0/24"
)

// runCommand executes a shell command and prints the output
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	log.Printf("Executing: %s %s", name, strings.Join(args, " "))

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("error running command `%s %s`: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// setupNetwork configures network interfaces and routes
func setupNetwork() error {
	log.Println("--- Configuring Network ---")

	// Configure tun11
	if err := runCommand("ip", "addr", "add", IFACE1_IP+"/"+IFACE1_CIDR, "dev", IFACE1_NAME); err != nil {
		return err
	}
	if err := runCommand("ip", "link", "set", "dev", IFACE1_NAME, "up"); err != nil {
		return err
	}

	// Configure tun22
	if err := runCommand("ip", "addr", "add", IFACE2_IP+"/"+IFACE2_CIDR, "dev", IFACE2_NAME); err != nil {
		return err
	}
	if err := runCommand("ip", "link", "set", "dev", IFACE2_NAME, "up"); err != nil {
		return err
	}

	log.Println("--- Network Configuration Complete ---")
	return nil
}

// forward copies data from one interface to another
func forward(src, dst *water.Interface) {
	packet := make([]byte, 65536) // MTU buffer
	for {
		n, err := src.Read(packet)
		if err != nil {
			log.Printf("Error reading from %s: %v", src.Name(), err)
			break
		}
		_, err = dst.Write(packet[:n])
		if err != nil {
			log.Printf("Error writing to %s: %v", dst.Name(), err)
			break
		}
	}
}

type packet struct {
	data []byte
}

func readFromTun(iface *water.Interface, ch chan<- packet) {
	defer close(ch)

	buffer := make([]byte, 65536)
	for {
		n, err := iface.Read(buffer)
		if err != nil {
			log.Printf("Error reading from %s: %v. Goroutine terminating.", iface.Name(), err)
			return
		}

		pktData := make([]byte, n)
		copy(pktData, buffer[:n])

		ch <- packet{data: pktData}
	}
}

func writeToTun(ch <-chan packet, iface *water.Interface) {
	for pkt := range ch {
		_, err := iface.Write(pkt.data)
		if err != nil {
			log.Printf("Error writing to %s: %v. Goroutine terminating.", iface.Name(), err)
			return
		}
	}
	log.Printf("Channel for %s closed. Writer goroutine finished.", iface.Name())
}
func forwardWithChannel(src, dst *water.Interface) {
	channel := make(chan packet, 2048)
	go readFromTun(src, channel)
	go writeToTun(channel, dst)
}

func Run(useChannel bool) {
	log.Printf("useChannel %t ", useChannel)
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root/sudo to configure network interfaces.")
	}

	// --- Create tun11 ---
	tun1, err := water.New(water.Config{
		DeviceType:             water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{Name: IFACE1_NAME},
	})
	if err != nil {
		log.Fatalf("Failed to create %s: %v", IFACE1_NAME, err)
	}
	defer tun1.Close()
	log.Printf("Interface %s created.", tun1.Name())

	// --- Create tun22 ---
	tun2, err := water.New(water.Config{
		DeviceType:             water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{Name: IFACE2_NAME},
	})
	if err != nil {
		log.Fatalf("Failed to create %s: %v", IFACE2_NAME, err)
	}
	defer tun2.Close()
	log.Printf("Interface %s created.", tun2.Name())

	// --- Configure network ---
	if err := setupNetwork(); err != nil {
		log.Fatalf("Failed to setup network: %v", err)
	}

	// --- Start bidirectional forwarding ---
	if useChannel {
		log.Println("Starting forwarding with channel...")
		forwardWithChannel(tun1, tun2)
		forwardWithChannel(tun2, tun1)
	} else {
		log.Println("Starting direct forwarding (no channel)...")
		go forward(tun1, tun2)
		go forward(tun2, tun1)
	}

	// --- Wait for termination signal ---
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutdown signal received.")
}
