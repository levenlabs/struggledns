package main

import (
	"strings"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
	"github.com/miekg/dns"
	"strconv"
)

var dnsServerGroups [][]string
var client dns.Client
var allowTruncated bool

type NewReq struct {
	Msg     *dns.Msg
	ReplyCh chan *dns.Msg
}

type RespReq struct {
	OrigMsg *dns.Msg
	Reply   *dns.Msg
}

var newCh = make(chan NewReq)
var respCh = make(chan RespReq)

//need to use init and not main for main_test.go
func init() {
	go reqSpin()
}

//Note: check len(msg.Answer) to make sure the response from this actually has an answer
func tryProxy(m *dns.Msg, addr string) *dns.Msg {
	kv := llog.KV{"addr": addr, "query": m.Question[0].Name}
	llog.Debug("calling exchange", kv)
	aM, _, err := client.Exchange(m, addr)
	if err != nil && (!allowTruncated || err != dns.ErrTruncated) {
		kv["err"] = err
		llog.Warn("forwarding error in tryProxy", kv)
		return nil
	}
	kv["rcode"] = aM.Rcode
	kv["answerCnt"] = len(aM.Answer)
	llog.Debug("exchange response", kv)
	return aM
}

func queryGroup(r *dns.Msg, servers []string) *dns.Msg {
	chs := make([]chan *dns.Msg, len(servers))
	for i := range servers {
		chs[i] = make(chan *dns.Msg, 1)
		go func(ch chan *dns.Msg, addr string) {
			ch <- tryProxy(r, addr)
		}(chs[i], servers[i])
	}

	var m *dns.Msg
	for i := range servers {
		if m = <-chs[i]; m != nil && len(m.Answer) > 0 {
			break
		}
	}
	return m
}

func queryAllGroups(r *dns.Msg) {
	var m *dns.Msg
	for i := range dnsServerGroups {
		m = queryGroup(r, dnsServerGroups[i])
		if m != nil && len(m.Answer) > 0 {
			break
		}
	}
	respCh <- RespReq{r, m}
}

func sendFormatError(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeFormatError)
	w.WriteMsg(m) //ignore write error
	return
}

func handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	kv := llog.KV{"question": "", "type": ""}
	var ok bool
	if len(r.Question) == 0 {
		llog.Warn("received request with no questions", kv)
		sendFormatError(w, r)
		return
	}
	kv["type"], ok = dns.TypeToString[r.Question[0].Qtype]
	if !ok || kv["type"] == "None" {
		kv["qtype"] = r.Question[0].Qtype
		llog.Warn("invalid type received", kv)
		sendFormatError(w, r)
		return
	}
	kv["question"] = r.Question[0].Name

	llog.Info("handling request", kv)
	rr := NewReq{r, make(chan *dns.Msg)}
	newCh <- rr
	m := <-rr.ReplyCh
	if m == nil {
		llog.Warn("error handling request", kv)
		dns.HandleFailed(w, r)
		return
	}

	//we need to make sure the sent ID matches the replied one
	//it might be different if we combined in-flight messages
	//copy the struct so we don't affect anything else handling this
	m2 := *m
	m2.Id = r.Id
	m = &m2

	//we always want to compress since there's no downsides afaik
	m.Compress = true

	kv["rcode"], _ = dns.RcodeToString[m.Rcode]
	if len(m.Answer) > 0 {
		kv["answer"] = m.Answer[0]
		kv["answerCnt"] = len(m.Answer)
	}
	kv["len"] = m.Len()
	llog.Info("responding to request", kv)

	err := w.WriteMsg(m)
	if err != nil {
		kv["err"] = err
		llog.Warn("error writing response", kv)
		//no need to handle HandleFailed here because we cannot write
	}
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
	l.Add(lever.Param{
		Name:        "--timeout",
		Description: "Timeout in milliseconds for each request",
		Default:     "300",
	})
	l.Add(lever.Param{
		Name:        "--log-level",
		Description: "Minimum log level to show, either debug, info, warn, error, or fatal",
		Default:     "warn",
	})
	l.Add(lever.Param{
		Name:        "--allow-truncated",
		Description: "If we should allow truncated responses to be proxied",
		Flag:        true,
	})
	l.Parse()

	addr, _ := l.ParamStr("--listen-addr")
	dnsServers, _ := l.ParamStrs("--fwd-to")
	combineGroups := l.ParamFlag("--parallel")
	timeout, _ := l.ParamInt("--timeout")

	logLevel, _ := l.ParamStr("--log-level")
	llog.SetLevelFromString(logLevel)

	allowTruncated = l.ParamFlag("--allow-truncated")

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

	client = dns.Client{
		//since this is UDP, the Dial/Write timeouts don't mean much
		//we really only care about setting the read
		DialTimeout:  time.Millisecond * 100,
		WriteTimeout: time.Millisecond * 100,
		ReadTimeout:  time.Millisecond * time.Duration(timeout),
		UDPSize: 4096,
	}

	handler := dns.HandlerFunc(handleRequest)
	go func() {
		llog.Info("listening on udp", llog.KV{"addr": addr})
		err := dns.ListenAndServe(addr, "udp", handler)
		llog.Fatal("error listening on udp", llog.KV{"err": err})
	}()
	go func() {
		llog.Info("listening on tcp", llog.KV{"addr": addr})
		err := dns.ListenAndServe(addr, "tcp", handler)
		llog.Fatal("error listening on tcp", llog.KV{"err": err})
	}()

	select {}
}

func getQuestionKey(q dns.Question) string {
	t := "nop"
	if t1, ok := dns.TypeToString[q.Qtype]; ok {
		t = t1
	}
	c := "nop"
	if c1, ok := dns.ClassToString[q.Qclass]; ok {
		c = c1
	}
	n := q.Name
	return n + t + c
}

func getMsgKey(r *dns.Msg) string {
	k := ""
	for _, q := range r.Question {
		k += getQuestionKey(q)
	}

	//RFC 1035 defines this as the max
	//since some clients (read: go) don't support edns0, this is the default
	//if they do send edns0 we'll raise it to what they sent
	limit := dns.MinMsgSize
	opt := r.IsEdns0()
	if opt != nil {
		limit = int(opt.UDPSize())
		llog.Debug("received edns0 limit", llog.KV{"limit": limit})
	}
	k += strconv.Itoa(limit)
	return k
}

func reqSpin() {
	var k string
	inFlight := make(map[string][]chan *dns.Msg)
	for {
		select {
		case r := <-newCh:
			k = getMsgKey(r.Msg)
			_, ok := inFlight[k]
			inFlight[k] = append(inFlight[k], r.ReplyCh)
			if !ok {
				go queryAllGroups(r.Msg)
			}
		case r := <-respCh:
			k = getMsgKey(r.OrigMsg)
			for _, ch := range inFlight[k] {
				ch <- r.Reply
			}
			delete(inFlight, k)
		}
	}
}
