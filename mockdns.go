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
	"strings"
	"sync"
	"syscall"

	"github.com/fatih/color"
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
	defaultTTL,
	resolvConfFile string
	proxy,
	verbose bool
	client             *dns.Client
	clientConfig       *dns.ClientConfig
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

	cFailure  = color.New(color.FgRed).Sprint("F")
	cSuccess  = color.New(color.FgGreen).Sprint("S")
	cOverride = color.New(color.FgYellow).Sprint("O")
	cProxied  = color.New(color.FgBlue).Sprint("P")
	cTerminal = color.New(color.FgRed).Sprint("T")
)

func init() {
	flag.StringVar(&addr, "addr", "127.0.0.1:8053", "default listening address")
	flag.StringVar(&dataFile, "data", "", "DNS record data file")
	flag.StringVar(&defaultTTL, "ttl", "3600", "default TTL")
	flag.StringVar(&resolvConfFile, "resolv", "/etc/resolv.conf", "resolv.conf file path")
	flag.BoolVar(&proxy, "proxy", true, "proxy unmatched requests to root name servers")
	flag.BoolVar(&verbose, "v", true, "verbose output")
}

func main() {
	flag.Parse()

	if dataFile == "" {
		log.Fatal("Data file required")
	}

	var err error
	if proxy {
		clientConfig, err = dns.ClientConfigFromFile(resolvConfFile)
		if err != nil {
			log.Fatalf("Reading %q: %s", resolvConfFile, err)
		}
		if len(clientConfig.Servers) == 0 {
			log.Fatalf("No name servers found in %q", resolvConfFile)
		}
		client = new(dns.Client)
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
		dns.HandleFunc(domain, logRequest(true, handler(recs)))
	}

	dns.HandleFunc(".", logRequest(false, proxyHandler))

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

func proxyHandler(w dns.ResponseWriter, r *dns.Msg) {
	var m *dns.Msg
	err := errors.New("not proxied")

	if proxy {
		for _, ns := range clientConfig.Servers {
			m, _, err = client.Exchange(r, fmt.Sprintf("%s:%s", ns, clientConfig.Port))
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		if m == nil {
			m = new(dns.Msg)
		}
		m.SetRcode(r, dns.RcodeServerFailure)
		r.Rcode = dns.RcodeServerFailure
	}

	w.WriteMsg(m)
}

func logRequest(local bool, f func(dns.ResponseWriter, *dns.Msg)) func(dns.ResponseWriter, *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		f(w, r)
		if verbose {
			var t, res string
			switch {
			case local:
				t = cOverride
			case proxy:
				t = cProxied
			default:
				t = cTerminal
			}

			// We don't have access to the reply Rcode, so we'll rely on the fact that
			// we mirror the reply Rcode to the request for its reference in middleware.
			if r.Rcode == 0 {
				res = cSuccess
			} else {
				res = cFailure
			}

			for _, q := range r.Question {
				log.Printf("[%s,%s]: %s", t, res, strings.TrimLeft(q.String(), ";"))
			}
		}
	}
}
