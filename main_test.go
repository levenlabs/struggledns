package main

import (
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"strings"
	. "testing"
)

var testDomain = "a-test.mysuperfancyapi.com."
var testAnyDomain = "any-test.mysuperfancyapi.com."

func init() {
	dnsServerGroups = [][]string{[]string{"8.8.8.8:53"}}
}

type Writer struct {
	ReplyCh chan *dns.Msg
}

func (w *Writer) LocalAddr() (a net.Addr) {
	return
}
func (w *Writer) RemoteAddr() (a net.Addr) {
	return
}
func (w *Writer) WriteMsg(r *dns.Msg) error {
	w.ReplyCh <- r
	return nil
}
func (w *Writer) Write(b []byte) (i int, err error) {
	r := new(dns.Msg)
	err = r.Unpack(b)
	w.ReplyCh <- r
	return
}
func (w *Writer) Close() error {
	return nil
}
func (w *Writer) TsigStatus() error {
	return nil
}
func (w *Writer) TsigTimersOnly(b bool) {}
func (w *Writer) Hijack()               {}

func getWriter() *Writer {
	return &Writer{make(chan *dns.Msg, 1)}
}

func Test(t *T) {
	m1 := new(dns.Msg)
	m1.SetQuestion(testDomain, dns.TypeA)
	w1 := getWriter()
	go func() {
		handleRequest(w1, m1)
	}()
	r1 := <-w1.ReplyCh
	require.Len(t, r1.Answer, 1)

	m2 := new(dns.Msg)
	m2.SetQuestion(testDomain, dns.TypeA)
	r2, err := dns.Exchange(m2, "8.8.8.8:53")
	require.Nil(t, err)
	require.Len(t, r2.Answer, 1)

	assert.Equal(t, r2.Rcode, r1.Rcode)
	a1 := strings.Split(r1.Answer[0].String(), "\t")
	//example: a-test.mysuperfancyapi.com., 245, IN, A, 192.95.20.208
	//we want to overwrite the TTL since that will be different
	a2 := strings.Split(r2.Answer[0].String(), "\t")
	a1[1] = ""
	a2[1] = ""
	assert.Equal(t, a2, a1)
}

func TestNXDOMAIN(t *T) {
	m1 := new(dns.Msg)
	m1.SetQuestion("-.", dns.TypeA)
	w1 := getWriter()
	go func() {
		handleRequest(w1, m1)
	}()
	r1 := <-w1.ReplyCh
	assert.Equal(t, dns.RcodeNameError, r1.Rcode)
}

func TestFORMERR(t *T) {
	m1 := new(dns.Msg)
	w1 := getWriter()
	go func() {
		handleRequest(w1, m1)
	}()
	r1 := <-w1.ReplyCh
	assert.Equal(t, dns.RcodeFormatError, r1.Rcode)

	m1 = new(dns.Msg)
	m1.SetQuestion(testDomain, dns.TypeNone)
	w1 = getWriter()
	go func() {
		handleRequest(w1, m1)
	}()
	r1 = <-w1.ReplyCh
	assert.Equal(t, dns.RcodeFormatError, r1.Rcode)
}

func TestInFlight(t *T) {
	m1 := new(dns.Msg)
	m1.SetQuestion(testDomain, dns.TypeA)
	w1 := getWriter()

	m2 := new(dns.Msg)
	m2.SetQuestion(testDomain, dns.TypeA)
	w2 := getWriter()

	go func() {
		handleRequest(w1, m1)
	}()
	go func() {
		handleRequest(w2, m2)
	}()
	var r1 *dns.Msg
	var r2 *dns.Msg
	for r1 == nil || r2 == nil {
		select {
		case r1 = <-w1.ReplyCh:
		case r2 = <-w2.ReplyCh:
		}
	}
	require.Equal(t, dns.RcodeSuccess, r1.Rcode)
	require.Len(t, r1.Answer, 1)
	assert.Equal(t, r2.Rcode, r2.Rcode)
	assert.Equal(t, r1.Answer[0], r2.Answer[0])
	assert.Equal(t, m1.Id, r1.Id)
	assert.Equal(t, m2.Id, r2.Id)
}

func TestInFlightAAAAAndA(t *T) {
	m1 := new(dns.Msg)
	m1.SetQuestion(testAnyDomain, dns.TypeAAAA)
	w1 := getWriter()

	m2 := new(dns.Msg)
	m2.SetQuestion(testAnyDomain, dns.TypeA)
	w2 := getWriter()

	go func() {
		handleRequest(w1, m1)
	}()
	go func() {
		handleRequest(w2, m2)
	}()
	var r1 *dns.Msg
	var r2 *dns.Msg
	for r1 == nil || r2 == nil {
		select {
		case r1 = <-w1.ReplyCh:
		case r2 = <-w2.ReplyCh:
		}
	}
	require.Len(t, r1.Answer, 1)
	require.Len(t, r2.Answer, 1)
	assert.NotEqual(t, r1.Answer[0], r2.Answer[0])
}

func TestInFlightEDns0(t *T) {
	m1 := new(dns.Msg)
	m1.SetQuestion(testAnyDomain, dns.TypeA)
	m1.SetEdns0(4096, false)
	w1 := getWriter()

	m2 := new(dns.Msg)
	m2.SetQuestion(testAnyDomain, dns.TypeA)
	w2 := getWriter()

	go func() {
		handleRequest(w1, m1)
	}()
	go func() {
		handleRequest(w2, m2)
	}()
	var r1 *dns.Msg
	var r2 *dns.Msg
	for r1 == nil || r2 == nil {
		select {
		case r1 = <-w1.ReplyCh:
		case r2 = <-w2.ReplyCh:
		}
	}
	assert.Equal(t, r1.Answer[0], r2.Answer[0])
	//note: this test could be flaky since we're relying on google to return
	//edns0 response when we send one vs when we don't send one
	assert.NotNil(t, r1.IsEdns0())
	assert.Nil(t, r2.IsEdns0())
}
