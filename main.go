package main

import (
	"log"
	"os"
	"strings"
	"tun_offload/normal_tun"
	"tun_offload/tun"
)

func main() {
	mode := ""
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-mode=") {
			mode = strings.TrimPrefix(arg, "-mode=")
		}
	}

	if mode == "normal" {
		normal_tun.Run()
	} else {
		tun.Run()
	}

	log.Println("Tun forward started. Press Ctrl+C to exit.")
	select {}
}
