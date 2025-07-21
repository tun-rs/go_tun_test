package tun

// Reference: https://github.com/WireGuard/wireguard-go/blob/master/tun/offload_linux.go

import (
	"fmt"
	"log"
	"os/exec"
)

// runCmd executes a shell command and returns any errors
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runCmd: %s %v: %v: %s", name, args, err, out)
	}
	return nil
}

// setupTun configures the TUN network interface
func setupTun(name, ip, cidr string) error {
	if err := runCmd("ip", "addr", "add", fmt.Sprintf("%s/%s", ip, cidr), "dev", name); err != nil {
		return err
	}
	if err := runCmd("ip", "link", "set", "dev", name, "up"); err != nil {
		return err
	}
	return nil
}

// forward copies data packets from src Device to dst Device in batches
func forward(src, dst Device) {
	const batch = 128
	const offset = 10
	const bufLen = 65535 + offset

	bufs := make([][]byte, batch)
	outBufs := make([][]byte, batch)
	sizes := make([]int, batch)
	for i := 0; i < batch; i++ {
		bufs[i] = make([]byte, bufLen)
	}
	for {
		// Ensure each bufs[i] is reset to full buffer length before each read
		for i := 0; i < batch; i++ {
			bufs[i] = bufs[i][:bufLen]
		}
		n, err := src.Read(bufs, sizes, offset)
		if err != nil {
			log.Printf("Read error: %v", err)
			continue
		}
		if n == 0 {
			continue
		}
		// outBufs slices only valid packet region for writing
		for i := 0; i < n; i++ {
			outBufs[i] = bufs[i][:sizes[i]+offset]
		}
		_, err = dst.Write(outBufs[:n], offset)
		if err != nil {
			log.Printf("Write error: %v", err)
			continue
		}
	}
}

// Run initializes two TUN devices and starts bidirectional forwarding between them
func Run() {
	// Create tun11
	tun1, err := CreateTUN("tun11", 1500)
	if err != nil {
		log.Fatalf("CreateTUN tun11 error: %v", err)
	}
	if err := setupTun("tun11", "10.0.1.1", "24"); err != nil {
		log.Fatalf("setup tun11 error: %v", err)
	}

	// Create tun22
	tun2, err := CreateTUN("tun22", 1500)
	if err != nil {
		log.Fatalf("CreateTUN tun22 error: %v", err)
	}
	if err := setupTun("tun22", "10.0.2.1", "24"); err != nil {
		log.Fatalf("setup tun22 error: %v", err)
	}

	// Start forwarding in both directions
	go forward(tun1, tun2)
	go forward(tun2, tun1)
}
