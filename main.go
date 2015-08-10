package main

import (
	"log"

	"github.com/mediocregopher/lever"
	"github.com/miekg/dns"
	"strings"
)

var dnsServerGroups [][]string

func tryProxy(m *dns.Msg, addr string) *dns.Msg {
	aM, err := dns.Exchange(m, addr)
	if err != nil {
		log.Printf("forwarding to %s got err: %s", addr, err)
		return nil
	} else if len(aM.Answer) == 0 {
		return nil
	}
	return aM
}

func queryServers(r *dns.Msg, servers []string) *dns.Msg {
	chs := make([]chan *dns.Msg, len(servers))
	for i := range servers {
		chs[i] = make(chan *dns.Msg, 1)
		go func(ch chan *dns.Msg, addr string) {
			ch <- tryProxy(r, addr)
		}(chs[i], servers[i])
	}

	var m *dns.Msg
	for i := range servers {
		if m = <-chs[i]; m != nil {
			break
		}
	}
	return m
}

func handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	var m *dns.Msg
	for i := range dnsServerGroups {
		m = queryServers(r, dnsServerGroups[i])
		if m != nil {
			break
		}
	}
	if m == nil {
		dns.HandleFailed(w, r)
		return
	}
	m.SetReply(r)
	w.WriteMsg(m)
}

func main() {

	l := lever.New("struggledns", nil)
	l.Add(lever.Param{
		Name:        "--listen-addr",
		Description: "Address to listen on for dns requests. Will bind to both tcp and udp",
		Default:     ":53",
	})
	l.Add(lever.Param{
		Name:        "--fwd-to",
		Description: "Address (ip:port) of a dns server to attempt forward requests to. Specify multiple times to make multiple request attempts. Order specified dictates precedence should more than one server respond for a request",
	})
	l.Add(lever.Param{
		Name:        "--parallel",
		Description: "If sent the query will be sent to all addresses in parallel",
		Flag:        true,
	})
	l.Parse()

	addr, _ := l.ParamStr("--listen-addr")
	dnsServers, _ := l.ParamStrs("--fwd-to")
	combineGroups := l.ParamFlag("--parallel")

	if combineGroups {
		//combine all the servers sent into one group
		dnsServerGroups = make([][]string, 1)
		var groupServers []string
		for i := range dnsServers {
			groupServers = strings.Split(dnsServers[i], ",")
			dnsServerGroups[0] = append(dnsServerGroups[0], groupServers...)
		}
	} else {
		dnsServerGroups = make([][]string, len(dnsServers))
		for i := range dnsServers {
			dnsServerGroups[i] = strings.Split(dnsServers[i], ",")
		}
	}

	handler := dns.HandlerFunc(handleRequest)
	go func() {
		log.Printf("Listening on %s (udp)", addr)
		log.Fatal(dns.ListenAndServe(addr, "udp", handler))
	}()
	go func() {
		log.Printf("Listening on %s (tcp)", addr)
		log.Fatal(dns.ListenAndServe(addr, "tcp", handler))
	}()

	select {}
}
