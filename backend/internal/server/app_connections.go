package server

import (
	"agentdeck/internal/relay"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"sync"
	"time"
)

type appDeviceStatus struct {
	Transports    []string `json:"transports"`
	IPs           []string `json:"ips"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Online        bool     `json:"online"`
	Connections   int      `json:"connections"`
	ConnectedAt   string   `json:"connected_at,omitempty"`
	LastConnected string   `json:"last_connected,omitempty"`
	PairedAt      string   `json:"paired_at"`
}
type appConnectionStatus struct {
	Online      int               `json:"online"`
	Paired      int               `json:"paired"`
	Connections int               `json:"connections"`
	Devices     []appDeviceStatus `json:"devices"`
}

// Count authenticated WebSockets, never tickets, pairing requests or the
// workstation/gateway tunnel. Several streams from one device remain one App.
type appStream struct {
	Info  func() (string, string)
	At    time.Time
	IP    string
	Close func()
}

func connectionIP(r *http.Request) string {
	value := r.RemoteAddr
	if relay.ContextGrant(r.Context()) != "" {
		value = r.Header.Get("X-AgentDeck-Client-IP")
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if ip, err := netip.ParseAddr(value); err == nil {
		return ip.Unmap().String()
	}
	return ""
}

func (s *Server) trackAppConnection(id, ip string, closeStream func(), info ...func() (string, string)) (func(), error) {
	at := time.Now().UTC()
	s.mu.Lock()
	if s.appStreams == nil {
		s.appStreams = map[string]map[uint64]appStream{}
	}
	var current struct {
		ID string `json:"id"`
	}
	if err := s.Store.Get("devices", id, &current); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.appStreams[id] == nil {
		s.appStreams[id] = map[uint64]appStream{}
	}
	s.appStreamID++
	stream := s.appStreamID
	row := appStream{At: at, IP: ip, Close: closeStream}
	if len(info) > 0 {
		row.Info = info[0]
	}
	s.appStreams[id][stream] = row
	s.mu.Unlock()
	_ = s.Store.RecordDeviceConnection(id, at)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.appStreams[id], stream)
			if len(s.appStreams[id]) == 0 {
				delete(s.appStreams, id)
			}
		})
	}, nil
}

func (s *Server) revokeDevice(id string) error {
	s.mu.Lock()
	if err := s.Store.Delete("devices", id); err != nil {
		s.mu.Unlock()
		return err
	}
	streams := s.appStreams[id]
	delete(s.appStreams, id)
	for token, ticket := range s.tickets {
		if ticket.Device == id {
			delete(s.tickets, token)
		}
	}
	s.mu.Unlock()
	for _, stream := range streams {
		stream.Close()
	}
	return nil
}

// This DTO is served only on the owner Unix socket and local console. Device
// roots, gateway grants and connection credentials never enter the response.
func (s *Server) appConnections() (appConnectionStatus, error) {
	out := appConnectionStatus{Devices: []appDeviceStatus{}}
	devices, err := s.Store.Devices()
	if err != nil {
		return out, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range devices {
		streams := s.appStreams[d.ID]
		row := appDeviceStatus{ID: d.ID, Name: d.Name, Online: len(streams) > 0, Connections: len(streams), LastConnected: d.LastConnected, PairedAt: d.Created}
		var earliest time.Time
		seenIPs := map[string]bool{}
		seenTransports := map[string]bool{}
		row.Transports = []string{}
		row.IPs = []string{}
		for _, stream := range streams {
			at := stream.At
			transport, ip := "https", stream.IP
			if stream.Info != nil {
				transport, ip = stream.Info()
			}
			if !seenTransports[transport] {
				row.Transports = append(row.Transports, transport)
				seenTransports[transport] = true
			}
			if ip != "" && !seenIPs[ip] {
				row.IPs = append(row.IPs, ip)
				seenIPs[ip] = true
			}
			if earliest.IsZero() || at.Before(earliest) {
				earliest = at
			}
		}
		sort.Strings(row.IPs)
		sort.Strings(row.Transports)
		if !earliest.IsZero() {
			row.ConnectedAt = earliest.Format(time.RFC3339Nano)
			out.Online++
		}
		out.Connections += len(streams)
		out.Devices = append(out.Devices, row)
	}
	out.Paired = len(out.Devices)
	sort.Slice(out.Devices, func(i, j int) bool {
		a, b := out.Devices[i], out.Devices[j]
		if a.Online != b.Online {
			return a.Online
		}
		if a.LastConnected != b.LastConnected {
			return a.LastConnected > b.LastConnected
		}
		return a.ID < b.ID
	})
	return out, nil
}
