package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wxiaoguang/echoping/internal/echoping"
)

func main() {
	var argListen, argListenTcp, argListenUdp string
	var argConnect, argLossRatio string

	flag.StringVar(&argListen, "listen", "", "Listen both TCP and UDP on ip:port (UDP also works for QUIC)")
	flag.StringVar(&argListenTcp, "listen-tcp", "", "Listen TCP on ip:port")
	flag.StringVar(&argListenUdp, "listen-udp", "", "Listen UDP on ip:port (UDP also works for QUIC)")
	flag.StringVar(&argConnect, "connect", "", "Connect to 'tcp://ip:port/,udp://ip:port/,quic://ip:port/' (can be repeated, use comma as delimiter), or use 'ip:port' for all TCP/UDP/QUIC")
	flag.DurationVar(&echoping.ClientPingInterval, "ping-interval", echoping.ClientPingInterval, "The interval between ping requests sent by client")
	flag.StringVar(&argLossRatio, "loss-ratio", argLossRatio, `The simulated UDP loss ratio on client side (must be used with "-connect"). "0.1"" means 10% packet loss, "0.1,0.2" means 0.1 for sending and 0.2 for receiving`)
	flag.Parse()

	if argListen != "" {
		if argListenTcp != "" || argListenUdp != "" {
			fmt.Printf("'-listen' can not co-exists with '-listen-tcp' or '-listen-udp'\n")
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
		if echoping.ClientPingInterval <= 0 || echoping.ClientPingInterval > 500*time.Millisecond {
			fmt.Printf("client ping interval should be between 0 and 500ms\n")
			os.Exit(2)
		}

		lossRatioFloats := []float64{0, 0}
		if argLossRatio != "" {
			lossRatios := strings.Split(argLossRatio, ",")
			if len(lossRatios) == 1 {
				lossRatios = append(lossRatios, lossRatios[0])
			}
			if len(lossRatios) != 2 {
				fmt.Printf("invalid client loss ratio\n")
				os.Exit(2)
			}
			for i := 0; i < 2; i++ {
				var err error
				if lossRatioFloats[i], err = strconv.ParseFloat(lossRatios[i], 64); err != nil {
					fmt.Printf("invalid client loss ratio: %s\n", lossRatios[i])
					os.Exit(2)
				}
			}
		}
		connectFields := strings.Split(argConnect, ",")
		var connectTargets []string
		for _, connectField := range connectFields {
			if strings.Index(connectField, "://") == -1 {
				connectTargets = append(connectTargets, "tcp://"+connectField)
				connectTargets = append(connectTargets, "udp://"+connectField)
				connectTargets = append(connectTargets, "quic://"+connectField)
			} else {
				connectTargets = append(connectTargets, connectField)
			}
		}
		for _, connectTarget := range connectTargets {
			u, err := url.Parse(connectTarget)
			if err != nil {
				fmt.Printf("can not parse connect target: %s, error: %v\n", connectTarget, err)
				os.Exit(2)
			}

			port := u.Port()
			if port == "" {
				fmt.Printf("can not parse connect target port: %s\n", connectTarget)
				os.Exit(2)
			}
			if u.Scheme == "tcp" {
				goServe(func() {
					client.ConnectEchoPingTcp(u.Hostname() + ":" + port)
				})
			} else if u.Scheme == "udp" || u.Scheme == "quic" {
				goServe(func() {
					client.ConnectEchoPingUdp(u.Scheme, u.Hostname()+":"+port, lossRatioFloats[0], lossRatioFloats[1])
				})
			} else {
				fmt.Printf("can not parse connect target protocol: %s\n", connectTarget)
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
