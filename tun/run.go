package tun

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

// --- Constants and Global Variables ---

const (
	batchSize  = 128
	offset     = 10
	mtuSize    = 1500
	maxPktSize = 65535 + offset
)

// packetBufferPool reuses packet buffers to reduce memory allocations and GC pressure.
// This will be initialized once if pooling is enabled.
var packetBufferPool *sync.Pool // Changed to a pointer to allow conditional initialization

// --- Helper Functions (No changes needed) ---

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runCmd: %s %v: %v: %s", name, args, err, out)
	}
	return nil
}

func setupTun(name, ip, cidr string) error {
	addr := fmt.Sprintf("%s/%s", ip, cidr)
	if err := runCmd("ip", "addr", "add", addr, "dev", name); err != nil {
		return err
	}
	if err := runCmd("ip", "link", "set", "dev", name, "up"); err != nil {
		return err
	}
	log.Printf("Successfully set up TUN device %s with address %s", name, addr)
	return nil
}

// --- Direct Forwarding (Original Method, with context) ---

func forward(src, dst Device) {
	const bufLen = mtuSize + offset

	bufs := make([][]byte, batchSize)
	outBufs := make([][]byte, batchSize)
	sizes := make([]int, batchSize)
	for i := 0; i < batchSize; i++ {
		bufs[i] = make([]byte, maxPktSize)
	}

	for {
		for i := 0; i < batchSize; i++ {
			bufs[i] = bufs[i][:bufLen]
		}
		n, err := src.Read(bufs, sizes, offset)
		if err != nil {
			log.Printf("Read error, exiting goroutine: %v", err)
			return
		}
		if n == 0 {
			continue
		}

		for i := 0; i < n; i++ {
			outBufs[i] = bufs[i][:sizes[i]+offset]
		}
		_, err = dst.Write(outBufs[:n], offset)
		if err != nil {
			log.Printf("Write error: %v", err)
		}
	}
}

// --- Channel-based Forwarding (Modified for conditional pooling) ---

type packet struct {
	buf  []byte
	size int
}

// readToChannel reads packets from a source device and sends them to a channel.
// It conditionally uses a sync.Pool based on whether packetBufferPool is initialized.
func readToChannel(src Device, ch chan<- packet) {
	readBufs := make([][]byte, batchSize)
	sizes := make([]int, batchSize)
	for i := 0; i < batchSize; i++ {
		readBufs[i] = make([]byte, mtuSize+offset)
	}

	usePool := packetBufferPool != nil // Check if the global pool is initialized

	for {
		n, err := src.Read(readBufs, sizes, offset)
		if err != nil {
			log.Printf("Read error, exiting goroutine: %v", err)
			return
		}

		for i := 0; i < n; i++ {
			var pktBuf []byte
			if usePool {
				pktBuf = packetBufferPool.Get().([]byte)
			} else {
				pktBuf = make([]byte, maxPktSize)
			}

			size := sizes[i] + offset

			// Defensive check, though should be rare if maxPktSize is correct.
			if cap(pktBuf) < size {
				log.Printf("Warning: Buffer obtained is too small. Allocating new.")
				pktBuf = make([]byte, size) // Fallback
				// Note: if this fallback happens and usePool is true, this specific buffer won't be returned to the pool.
			}

			copy(pktBuf, readBufs[i][:size])
			ch <- packet{buf: pktBuf, size: size}
		}
	}
}

// writeFromChannel receives packets from a channel and writes them to a destination device.
// It conditionally returns buffers to the sync.Pool.
func writeFromChannel(dst Device, ch <-chan packet) {
	bufs := make([][]byte, batchSize)
	originalBufs := make([][]byte, batchSize) // To store original pooled buffers

	usePool := packetBufferPool != nil // Check if the global pool is initialized
	for pkt := range ch {
		var n int
		bufs[0] = pkt.buf[:pkt.size]
		if usePool {
			originalBufs[0] = pkt.buf // Store the original buffer for returning to pool
		}

		// Collect more packets if they are immediately available.
	collect:
		for n = 1; n < batchSize; n++ {
			select {
			case pkt := <-ch:
				bufs[n] = pkt.buf[:pkt.size]
				if usePool {
					originalBufs[n] = pkt.buf
				}
			default:
				break collect
			}
		}

		if n > 0 {
			_, err := dst.Write(bufs[:n], offset)
			if err != nil {
				log.Printf("Write error: %v", err)
			}

			// IMPORTANT: Conditionally return all used buffers to the pool.
			if usePool {
				for i := 0; i < n; i++ {
					// Return the original full buffer, not the sliced one.
					packetBufferPool.Put(originalBufs[i])
				}
			}
		}
	}
}

// forwardWithChannel sets up the goroutines for channel-based forwarding.
// The usePool parameter now controls the global packetBufferPool initialization.
func forwardWithChannel(src, dst Device, usePool bool) {
	// Initialize the global pool if usePool is true and it hasn't been initialized yet.
	// This ensures it's only initialized once.
	if usePool && packetBufferPool == nil {
		// Use a sync.Once or similar if multiple calls to forwardWithChannel are possible
		// and you want to strictly guarantee single initialization.
		// For this specific setup (Run calls it once per direction),
		// a simple nil check is sufficient.
		packetBufferPool = &sync.Pool{
			New: func() interface{} {
				return make([]byte, maxPktSize)
			},
		}
		log.Println("Initialized sync.Pool for packet buffers.")
	} else if !usePool && packetBufferPool != nil {
		// If we're turning off pooling, clear the global pool reference
		// to ensure read/write functions don't try to use it.
		// This is less common, usually you decide at startup and stick to it.
		packetBufferPool = nil
		log.Println("Disabled sync.Pool for packet buffers.")
	}

	channel := make(chan packet, 2048)
	go readToChannel(src, channel)
	go writeFromChannel(dst, channel)
}

// --- Main Execution Logic (Modified) ---

func Run(useChannel bool, usePool bool) { // Added usePool parameter
	log.Printf("Starting TUN forwarding demo (useChannel: %t, usePool: %t)", useChannel, usePool)

	tun1, err := CreateTUN("tun11", mtuSize)
	if err != nil {
		log.Fatalf("Failed to create tun11: %v", err)
	}
	defer tun1.Close()
	if err := setupTun("tun11", "10.0.1.1", "24"); err != nil {
		log.Fatalf("Failed to setup tun11: %v", err)
	}

	tun2, err := CreateTUN("tun22", mtuSize)
	if err != nil {
		log.Fatalf("Failed to create tun22: %v", err)
	}
	defer tun2.Close()
	if err := setupTun("tun22", "10.0.2.1", "24"); err != nil {
		log.Fatalf("Failed to setup tun22: %v", err)
	}

	if useChannel {
		log.Println("Using channel-based forwarding.")
		// The forwardWithChannel now handles the global pool initialization/clearing
		forwardWithChannel(tun1, tun2, usePool) // Pass usePool here
		forwardWithChannel(tun2, tun1, usePool) // And here
	} else {
		log.Println("Using direct forwarding (sync.Pool not applicable here).")
		// For direct forwarding, the usePool parameter has no effect as it doesn't use the channel-based logic.
		go forward(tun1, tun2)
		go forward(tun2, tun1)
	}

	log.Println("Forwarding started. Press Ctrl+C to exit.")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	log.Println("Shutdown complete.")
}

