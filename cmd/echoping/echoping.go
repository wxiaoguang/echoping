package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wxiaoguang/echoping/internal/echoping"
)

func main() {
	var argListen, argListenTcp, argListenUdp string
	var argConnect string

	flag.StringVar(&argListen, "listen", "", "listen both TCP and UDP on ip:port")
	flag.StringVar(&argListenTcp, "listen-tcp", "", "listen TCP on ip:port")
	flag.StringVar(&argListenUdp, "listen-udp", "", "listen UDP on ip:port")
	flag.StringVar(&argConnect, "connect", "", "connect to 'tcp://ip:port/,udp://ip:port/' (can be repeated, use comma as delimiter), or use 'ip:port' for both TCP and UDP")
	flag.DurationVar(&echoping.ClientPingInterval, "ping-interval", echoping.ClientPingInterval, "the interval between ping requests sent by client")
	flag.Parse()

	if argListen != "" {
		if argListenTcp != "" || argListenUdp != "" {
			fmt.Printf("'-listen' can not co-exists with '-listen-tcp' or '-listen-udp'")
			os.Exit(1)
		}
		argListenTcp = argListen
		argListenUdp = argListen
	}

	servedCount := 0
	goServe := func(fn func()) {
		servedCount++
		go fn()
	}

	server := echoping.NewServer()
	client := echoping.NewClient()

	if argListenTcp != "" {
		goServe(func() {
			server.ServeEchoPingTcp(argListenTcp)
		})
	}
	if argListenUdp != "" {
		goServe(func() {
			server.ServeEchoPingUdp(argListenUdp)
		})
	}
	if argConnect != "" {
		if echoping.ClientPingInterval <= 0 || echoping.ClientPingInterval > 500 * time.Millisecond {
			fmt.Printf("client ping interval should be between 0 and 500ms")
			os.Exit(2)
		}
		connectFields := strings.Split(argConnect, ",")
		var connectTargets []string
		for _, connectField := range connectFields {
			if strings.Index(connectField, "://") == -1 {
				connectTargets = append(connectTargets, "tcp://" + connectField)
				connectTargets = append(connectTargets, "udp://" + connectField)
			} else {
				connectTargets = append(connectTargets, connectField)
			}
		}
		for _, connectTarget := range connectTargets {
			u, err := url.Parse(connectTarget)
			if err != nil {
				fmt.Printf("can not parse connect target: %s, error: %v", connectTarget, err)
				os.Exit(2)
			}

			port := u.Port()
			if port == "" {
				fmt.Printf("can not parse connect target port: %s", connectTarget)
				os.Exit(2)
			}
			if u.Scheme == "tcp" {
				goServe(func() {
					client.ConnectEchoPingTcp(u.Hostname() + ":" + port)
				})
			} else if u.Scheme == "udp" {
				goServe(func() {
					client.ConnectEchoPingUdp(u.Hostname() + ":" + port)
				})
			} else {
				fmt.Printf("can not parse connect target protocol: %s", connectTarget)
				os.Exit(2)
			}
		}
	}

	if servedCount > 0 {
		select {} // wait forever
	} else {
		flag.PrintDefaults()
	}
}
