package agent

import (
	"agentdeck/internal/store"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Root struct {
	Agent  string
	Path   string
	Source string
}
type Follower struct {
	Normalizer   *Normalizer
	Roots        []Root
	Notify       func(store.Event)
	Lookback     time.Duration
	HistoryBytes int64
	Index        string
	indexStamp   string
	states       map[string]*State
	stamps       map[string]string
}

func (f *Follower) Run(ctx context.Context) {
	f.states = map[string]*State{}
	f.stamps = map[string]string{}
	f.Scan()
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.Scan()
		}
	}
}
func (f *Follower) Scan() {
	if f.states == nil {
		f.states = map[string]*State{}
		f.stamps = map[string]string{}
	}
	for _, r := range f.Roots {
		type entry struct {
			path     string
			modified time.Time
		}
		files := []entry{}
		_ = filepath.WalkDir(r.Path, func(path string, d os.DirEntry, e error) error {
			if e != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") && !strings.HasSuffix(path, ".ndjson") {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return nil
			}
			if f.Lookback > 0 && time.Since(info.ModTime()) > f.Lookback {
				return nil
			}
			files = append(files, entry{path, info.ModTime()})
			return nil
		})
		sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
		for _, file := range files {
			if e := f.read(file.path, r); e != nil {
				log.Printf("source read failed agent=%s file_id=%s err=%v", r.Agent, digest(file.path)[:12], e)
			}
		}
	}

	f.refreshTitles()
}
func (f *Follower) read(path string, r Root) error {
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	stamp := fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
	k := digest(path)
	st := f.states[path]
	if st == nil {
		st = &State{Session: k[:24]}
		if e = f.Normalizer.Store.Get("checkpoints", k, st); e != nil && !os.IsNotExist(e) {
			return e
		}
		f.states[path] = st
	}
	var tracked store.Session
	desktop := f.Normalizer.Store.Get("sessions", st.Session, &tracked) == nil && tracked.Managed && tracked.Source == "desktop"
	if desktop && st.DesktopSync < 1 {
		// Older versions advanced the checkpoint while dropping every record
		// after desktop adoption. Re-read the bounded recent window once;
		// deterministic source event IDs deduplicate previously saved records.
		st.Offset = 0
		st.DesktopSync = 1
		delete(f.stamps, path)
	}
	if f.stamps[path] == stamp {
		return nil
	}
	if st.MediaVersion < 1 {
		if st.Offset > 0 {
			if err := f.recoverImages(path, r, st); err != nil {
				return err
			}
		}
		st.MediaVersion = 1
	}
	if info.Size() < st.Offset {
		*st = State{Session: k[:24]}
	}
	if st.CWD != "" {
		_ = f.Normalizer.Store.SetProject(st.Session, st.CWD)
	}
	file, e := os.Open(path)
	if e != nil {
		return e
	}
	defer file.Close()
	if _, e = file.Seek(st.Offset, 0); e != nil {
		return e
	}
	// On first import, keep a bounded recent window; first-line metadata retains
	// the true session identity. Existing checkpoints always resume exactly.
	if st.Offset == 0 && f.HistoryBytes > 0 && info.Size() > f.HistoryBytes {
		first := bufio.NewReader(file)
		line, _, _, e := readRecord(first)
		if e == nil {
			_, _ = f.Normalizer.Parse(line, r.Agent, r.Source, st)
		}
		start := info.Size() - f.HistoryBytes
		if _, e = file.Seek(start, 0); e != nil {
			return e
		}
		tail := bufio.NewReader(file)
		_, consumed, _, e := readRecord(tail)
		if e != nil && e != io.EOF {
			return e
		}
		st.Offset = start + consumed
		if _, e = file.Seek(st.Offset, 0); e != nil {
			return e
		}
	}
	reader := bufio.NewReader(file)
	for count := 0; count < 1000; count++ {
		start := st.Offset
		line, consumed, oversized, e := readRecord(reader)
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		st.Offset += consumed
		if oversized {
			log.Printf("source record skipped: exceeds 16MiB agent=%s", r.Agent)
			continue
		}
		out, e := f.Normalizer.Parse(line, r.Agent, r.Source, st)
		if e != nil {
			log.Printf("malformed source record agent=%s offset=%d", r.Agent, start)
			continue
		}
		if st.CWD != "" {
			_ = f.Normalizer.Store.SetProject(st.Session, st.CWD)
		}
		var existing store.Session
		if f.Normalizer.Store.Get("sessions", st.Session, &existing) == nil && existing.Managed && existing.Source != "desktop" {
			continue
		}
		sum := sha256.Sum256(line)
		for i, ev := range out {
			if ev.Session == "" {
				ev.Session = k[:24]
			}
			ev.ID = digest(k + fmt.Sprint(start) + hex.EncodeToString(sum[:]) + fmt.Sprint(i))
			var stored store.Event
			var added bool
			var e error
			if r.Agent == "codex" {
				stored, added, e = f.Normalizer.Store.AppendDesktop(ev, false)
			} else {
				stored, added, e = f.Normalizer.Store.Append(ev)
			}
			if e != nil {
				st.Offset = start
				return e
			}
			if added && f.Notify != nil {
				f.Notify(stored)
			}
		}
	}
	if e = f.Normalizer.Store.Put("checkpoints", k, st); e != nil {
		return e
	}
	if st.Offset >= info.Size() {
		f.stamps[path] = stamp
	}
	return nil
}

// Drain oversized records without unbounded allocation, and leave unterminated
// records uncommitted so a later append can complete them.
func readRecord(r *bufio.Reader) ([]byte, int64, bool, error) {
	var out []byte
	var size int64
	oversized := false
	for {
		part, e := r.ReadSlice('\n')
		size += int64(len(part))
		if size > 16<<20 {
			oversized = true
			out = nil
		} else if !oversized {
			out = append(out, part...)
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		return out, size, oversized, e
	}
}

func (f *Follower) refreshTitles() {
	if f.Index == "" {
		return
	}
	info, e := os.Stat(f.Index)
	if e != nil {
		return
	}
	stamp := fmt.Sprint(info.ModTime().UnixNano(), "/", info.Size())
	if stamp == f.indexStamp {
		return
	}
	ss, e := f.Normalizer.Store.Sessions()
	if e != nil {
		return
	}
	active := map[string]bool{}
	for _, v := range ss {
		if v.Agent == "codex" {
			active[v.ID] = true
		}
	}
	file, e := os.Open(f.Index)
	if e != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	latest := map[string]map[string]any{}
	for scanner.Scan() {
		var v map[string]any
		if json.Unmarshal(scanner.Bytes(), &v) == nil && active[str(v["id"])] && str(v["thread_name"]) != "" {
			latest[str(v["id"])] = v
		}
	}
	if scanner.Err() != nil {
		return
	}
	for id, v := range latest {
		ev := store.Event{ID: digest("title/" + id + str(v["thread_name"]) + str(v["updated_at"])), Session: id, Agent: "codex", Kind: "status", Mode: "title", Text: Redact(str(v["thread_name"])), Source: "log"}
		if _, _, e = f.Normalizer.Store.Append(ev); e != nil {
			return
		}
	}
	f.indexStamp = stamp
}

// Recover images from the bounded imported history, without replaying messages.
func (f *Follower) recoverImages(path string, r Root, checkpoint *State) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	state := State{Session: checkpoint.Session, CWD: checkpoint.CWD}
	existing := map[string]bool{}
	cursor := uint64(0)
	for {
		events, e := f.Normalizer.Store.Events(checkpoint.Session, cursor, 100)
		if e != nil {
			return e
		}
		if len(events) == 0 {
			break
		}
		for _, ev := range events {
			cursor = ev.Seq
			if ev.Kind == "image" {
				existing[ev.Item+":"+ev.Media] = true
			}
		}
	}
	reader := bufio.NewReader(file)
	line, _, oversized, e := readRecord(reader)
	if e != nil && e != io.EOF {
		return e
	}
	if !oversized && e == nil {
		_, _ = f.Normalizer.Parse(line, r.Agent, r.Source, &state)
	}
	start := int64(0)
	limit := f.HistoryBytes
	if limit <= 0 {
		limit = 8 << 20
	}
	if checkpoint.Offset > limit {
		start = checkpoint.Offset - limit
		if _, e = file.Seek(start, 0); e != nil {
			return e
		}
		reader = bufio.NewReader(file)
		_, used, _, e := readRecord(reader)
		if e != nil && e != io.EOF {
			return e
		}
		start += used
	} else {
		if _, e = file.Seek(0, 0); e != nil {
			return e
		}
	}
	reader = bufio.NewReader(file)
	for start < checkpoint.Offset {
		line, used, oversized, e := readRecord(reader)
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		position := start
		start += used
		if oversized {
			continue
		}
		out, e := f.Normalizer.Parse(line, r.Agent, r.Source, &state)
		if e != nil {
			continue
		}
		for _, ev := range out {
			if ev.Kind != "image" || existing[ev.Item+":"+ev.Media] {
				continue
			}
			ev.ID = digest("media-recovery-v1:" + path + fmt.Sprint(position) + ev.Item + ev.Media)
			stored, added, e := f.Normalizer.Store.AppendRecoveredImage(ev)
			if e != nil {
				return e
			}
			existing[ev.Item+":"+ev.Media] = true
			if added && f.Notify != nil {
				f.Notify(stored)
			}
		}
	}
	return nil
}
