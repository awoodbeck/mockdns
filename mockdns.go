package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/miekg/dns"
)

const (
	keyHostname = "hostname"
	keyPriority = "priority"
	keyTTL      = "ttl"
	keyValue    = "value"
)

var (
	addr,
	dataFile,
	defaultTTL string
	errNilMapUnmarshal = errors.New("cannot unmarshal into nil map")

	supportedTypes = map[string]uint16{
		"A":     dns.TypeA,
		"AAAA":  dns.TypeAAAA,
		"CAA":   dns.TypeCAA,
		"CNAME": dns.TypeCNAME,
		"MX":    dns.TypeMX,
		"NS":    dns.TypeNS,
		"PTR":   dns.TypePTR,
		"TXT":   dns.TypeTXT,
	}
)

func init() {
	flag.StringVar(&addr, "addr", "127.0.0.1:8053", "default listening address")
	flag.StringVar(&dataFile, "data", "", "DNS record data file")
	flag.StringVar(&defaultTTL, "ttl", "3600", "default TTL")
}

func main() {
	flag.Parse()

	if dataFile == "" {
		log.Fatal("Data file required")
	}

	b, err := ioutil.ReadFile(dataFile)
	if err != nil {
		log.Fatal(err)
	}

	d := make(data)
	err = json.Unmarshal(b, &d)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	for _, net := range []string{"tcp", "udp"} {
		wg.Add(1)
		go func(net string) {
			serve(ctx, addr, net, d)
			wg.Done()
		}(net)
	}

	chs := make(chan os.Signal, 1)
	signal.Notify(chs, syscall.SIGINT, syscall.SIGTERM)
	s := <-chs
	fmt.Println()
	log.Printf("Received %q signal; stopping ...\n", s)
	cancel()
	wg.Wait()
}

func serve(ctx context.Context, addr, net string, d data) {
	for domain, recs := range d {
		dns.HandleFunc(domain, handler(recs))
	}

	server := &dns.Server{Addr: addr, Net: net, TsigSecret: nil}

	go func() {
		<-ctx.Done()
		err := server.Shutdown()
		if err != nil {
			log.Println(err)
		}
	}()

	log.Printf("Listening on %s/%s ...\n", addr, net)
	err := server.ListenAndServe()
	if err != nil {
		log.Println(err)
	}
	log.Printf("%s/%s listener stopped\n", addr, net)
}

func handler(recs records) func(dns.ResponseWriter, *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)

		log.Printf("Incoming request: %#v\n", r)

		// answer
		for _, question := range r.Question {
			if question.Qtype == dns.TypeANY {
				for _, rrs := range recs.data {
					m.Answer = append(m.Answer, rrs...)
				}
			} else {
				if rrs, ok := recs.data[question.Qtype]; ok {
					m.Answer = append(m.Answer, rrs...)
				}
			}
		}

		// authority
		if rrs, ok := recs.data[dns.TypeNS]; ok {
			m.Ns = append(m.Ns, rrs...)
		}

		// additional
		if rrs, ok := recs.data[dns.TypeA]; ok {
			m.Extra = append(m.Extra, rrs...)
		}
		if rrs, ok := recs.data[dns.TypeAAAA]; ok {
			m.Extra = append(m.Extra, rrs...)
		}

		w.WriteMsg(m)
	}
}
