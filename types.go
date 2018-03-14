package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/miekg/dns"
)

type data map[string]records

func (d data) UnmarshalJSON(b []byte) error {
	if d == nil {
		return errNilMapUnmarshal
	}

	var m map[string]json.RawMessage

	err := json.Unmarshal(b, &m)
	if err == nil {
		for domain, j := range m {
			domain = dns.Fqdn(strings.ToLower(domain))

			rt := records{
				fqdn: domain,
				data: make(map[uint16][]dns.RR),
			}
			uErr := json.Unmarshal(j, &rt)
			if uErr != nil {
				return uErr
			}

			d[domain] = rt
		}
	}

	return err
}

type records struct {
	fqdn string
	data map[uint16][]dns.RR
}

func (recs *records) UnmarshalJSON(b []byte) error {
	if recs.data == nil {
		recs.data = make(map[uint16][]dns.RR)
	}

	var m map[string][]map[string]string
	err := json.Unmarshal(b, &m)
	if err == nil {
		for typ, v := range m {
			for _, r := range v {
				typ = strings.ToUpper(typ)
				iType, ok := supportedTypes[typ]
				if !ok {
					continue // unsupported type
				}

				rr, rErr := recs.rrFromMap(typ, recs.fqdn, r)
				if rErr != nil {
					return rErr
				}
				if rr != nil {
					recs.data[iType] = append(recs.data[iType], rr)
				}
			}
		}
	}

	return err
}

func (recs records) rrFromMap(typ, fqdn string, m map[string]string) (dns.RR, error) {
	if m == nil {
		return nil, nil
	}

	var parts []string

	if v, ok := m[keyHostname]; ok {
		if v == "@" { // wildcard host name
			v = fqdn
		} else if !strings.HasSuffix(v, fqdn) {
			v = fmt.Sprintf("%s.%s", v, fqdn)
		}
		parts = append(parts, v)
	} else {
		parts = append(parts, fqdn)
	}

	if v, ok := m[keyTTL]; ok {
		parts = append(parts, v)
	} else {
		parts = append(parts, defaultTTL)
	}

	parts = append(parts, "IN", typ)

	if typ == "MX" { // priority only support for MX records
		if v, ok := m[keyPriority]; ok {
			parts = append(parts, v)
		}
	}

	if v, ok := m[keyValue]; ok {
		if typ == "TXT" {
			v = fmt.Sprintf("%q", v)
		}
		parts = append(parts, v)
	}

	s := strings.Join(parts, " ")
	rr, err := dns.NewRR(s)

	if err != nil {
		log.Printf("Attempting to parse: %q", s)
	}

	return rr, err
}
