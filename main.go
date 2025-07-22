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
	useChannel := false
	usePool := false

	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-mode=") {
			mode = strings.TrimPrefix(arg, "-mode=")
		} else if arg == "-useChannel" {
			useChannel = true
		} else if arg == "-usePool" {
			usePool = true
		}
	}

	if mode == "normal" {
		normal_tun.Run(useChannel)
	} else {
		tun.Run(useChannel, usePool)
	}

	log.Println("Tun forward started. Press Ctrl+C to exit.")
	select {}
}
