package main

import (
	"log"

	"github.com/mediocregopher/lever"
	"github.com/miekg/dns"
)

var dnsServers []string

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

func handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	chs := make([]chan *dns.Msg, len(dnsServers))
	for i := range dnsServers {
		chs[i] = make(chan *dns.Msg, 1)
		go func(ch chan *dns.Msg, addr string) {
			ch <- tryProxy(r, addr)
		}(chs[i], dnsServers[i])
	}

	var m *dns.Msg
	for i := range dnsServers {
		if m = <-chs[i]; m != nil {
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
	l.Parse()

	addr, _ := l.ParamStr("--listen-addr")
	dnsServers, _ = l.ParamStrs("--fwd-to")

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
