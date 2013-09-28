// Bridges UDP to IRC.
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	irc "github.com/calmh/goirc/client"
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

var (
	server         = flag.String("server", "localhost:6667", "IRC server to connect to, host:port")
	useSSL         = flag.Bool("ssl", false, "Use SSL")
	useInsecureSSL = flag.Bool("ssl-insecure", false, "Ignore certificate validity check")
	nick           = flag.String("nick", "ircbridge", "IRC nick")
	realname       = flag.String("realname", "IRC Bridge", "IRC realname")
	udpPort        = flag.Uint("listen", 41234, "JSON-UDP listen port")
	debug          = flag.Bool("debug", false, "Print debug info")
	reconnect      = flag.Duration("reconnect", 60*time.Second, "Reconnect interval (delay)")
)

var clientLock sync.Mutex
var joinedChannels = make(map[string]struct{})

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

func handleCommands(cmdChan chan Command, client *irc.Conn) {
	for {
		cmd := <-cmdChan
		handleCommand(cmd, client)
	}
}

func handleCommand(cmd Command, client *irc.Conn) {
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
	if *debug {
		log.Printf("debug: handled: %#v", cmd)
	}
}

func checkParams(cmd Command, np int) bool {
	if len(cmd.Params) < np {
		log.Printf("warning: invalid command: %#v", cmd)
		return false
	}
	return true
}

func connectIRC(server string, useSSL bool, useInsecureSSL bool, nick string, realname string) *irc.Conn {
	cfg := irc.NewConfig(nick, nick, realname)
	cfg.SSL = useSSL
	cfg.SSLConfig = &tls.Config{InsecureSkipVerify: useInsecureSSL}
	cfg.Server = server

	c := irc.Client(cfg)

	c.HandleFunc("disconnected", handleDisconnects)
	c.HandleFunc("connected", handleReconnects)

	err := c.Connect()
	if err != nil {
		log.Fatal(err)
	}

	return c
}

func handleDisconnects(conn *irc.Conn, line *irc.Line) {
	log.Println("warning: disconnected from IRC")
	defer clientLock.Unlock()
	clientLock.Lock()

	for {
		time.Sleep(*reconnect)

		log.Println("notice: reconnecting")
		err := conn.Connect()

		if err == nil {
			log.Println("notice: reconnected")
			return
		}
		log.Println(err)
	}
}

func handleReconnects(conn *irc.Conn, line *irc.Line) {
	defer clientLock.Unlock()
	clientLock.Lock()

	for c := range joinedChannels {
		log.Println("rejoining", c)
		conn.Join(c)
	}
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
