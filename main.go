// Bridges UDP to IRC.
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	irc "github.com/calmh/ircbridged/github.com/fluffle/goirc/client"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

type Command struct {
	Method string
	Params []string
}

const reconnectInterval = 10 * time.Second

var (
	server         = flag.String("irc.server", "localhost:6667", "IRC server to connect to, host:port")
	useSSL         = flag.Bool("irc.ssl", false, "Use SSL")
	useInsecureSSL = flag.Bool("irc.ssl.insecure", false, "Ignore certificate validity check")
	nick           = flag.String("irc.nick", "ircbridge", "IRC nick")
	realname       = flag.String("irc.realname", "IRC Bridge", "IRC realname")
	udpPort        = flag.Uint("udp.port", 41234, "JSON-UDP listen port")
	debug          = flag.Bool("debug", false, "Print debug info")
)

var clientLock sync.Mutex
var joinedChannels = make(map[string]struct{})

func connectIRC(server string, useSSL bool, useInsecureSSL bool, nick string, realname string) *irc.Conn {
	c := irc.SimpleClient(nick, nick, realname)
	c.SSL = useSSL
	c.SSLConfig = &tls.Config{InsecureSkipVerify: useInsecureSSL}

	c.AddHandler(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		log.Println("warning: disconnected from IRC")
		defer clientLock.Unlock()
		clientLock.Lock()

		for {
			time.Sleep(reconnectInterval)

			log.Println("notice: reconnecting")
			err := conn.Connect(server)

			if err == nil {
				log.Println("notice: reconnected")
				return
			}
			log.Println(err)
		}
	})

	c.AddHandler(irc.INIT, func(conn *irc.Conn, line *irc.Line) {
		defer clientLock.Unlock()
		clientLock.Lock()
		for c := range joinedChannels {
			log.Println("rejoining", c)
			conn.Join(c)
		}
	})

	err := c.Connect(server)
	if err != nil {
		log.Fatal(err)
	}

	return c
}

func recvUdp(cmdChan chan Command, conn io.Reader) {
	for {
		bs := make([]byte, 10240)
		n, err := conn.Read(bs)
		if err != nil {
			log.Fatal(err)
		}

		if *debug {
			log.Printf("debug: recv: %q", string(bs[:n]))
		}

		var cmd Command
		err = json.Unmarshal(bs[:n], &cmd)
		if err != nil {
			log.Println("warning:", err)
		}

		if *debug {
			log.Printf("debug: recv (parsed): %#v", cmd)
		}
		cmdChan <- cmd
	}
}

func handleCommands(cmdChan chan Command, client *irc.Conn) {
	for {
		cmd := <-cmdChan

		func(cmd Command) {
			defer clientLock.Unlock()
			clientLock.Lock()
			switch cmd.Method {
			case "message":
				if checkParams(cmd, 2) {
					client.Privmsg(cmd.Params[0], cmd.Params[1])
				}
			case "notice":
				if checkParams(cmd, 2) {
					client.Notice(cmd.Params[0], cmd.Params[1])
				}
			case "join":
				if checkParams(cmd, 1) {
					client.Join(cmd.Params[0])
					joinedChannels[cmd.Params[0]] = struct{}{}
				}
			case "part":
				if checkParams(cmd, 1) {
					client.Part(cmd.Params[0])
					delete(joinedChannels, cmd.Params[0])
				}
			}
		}(cmd)

		if *debug {
			log.Printf("debug: handled: %#v", cmd)
		}
	}
}

func main() {
	flag.Parse()

	addr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(int(*udpPort)))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	client := connectIRC(*server, *useSSL, *useInsecureSSL, *nick, *realname)

	cmdChan := make(chan Command)
	go recvUdp(cmdChan, conn)
	handleCommands(cmdChan, client)
}

func checkParams(cmd Command, np int) bool {
	if len(cmd.Params) < np {
		log.Printf("warning: invalid command: %#v", cmd)
		return false
	}
	return true
}
