// Selected-state commands: FETCH, STORE, SEARCH, EXPUNGE, COPY, MOVE.
package imapd

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	gotextproto "github.com/emersion/go-message/textproto"

	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func (s *session) requireSelected() (*view, error) {
	if s.view == nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "no mailbox selected"}
	}
	return s.view, nil
}

// ---- FETCH ----

func (s *session) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	v, err := s.requireSelected()
	if err != nil {
		return err
	}
	markSeen := false
	for _, bs := range options.BodySection {
		if !bs.Peek {
			markSeen = true
			break
		}
	}

	type fetchedItem struct {
		seq uint32
		m   msgInfo
		raw []byte
	}
	var fetched []fetchedItem
	seen := map[uint32]struct{}{}
	v.forEach(numSet, func(seq uint32, m msgInfo) {
		seen[m.UID] = struct{}{}
		fetched = append(fetched, fetchedItem{seq: seq, m: m})
	})

	for _, fi := range fetched {
		raw, rerr := s.srv.loadRaw(context.Background(), fi.m.MessageID)
		if rerr != nil {
			continue // skip unreadable bodies rather than failing the whole FETCH
		}
		resp := w.CreateMessage(v.st.EncodeSeqNum(fi.seq))
		resp.WriteUID(imap.UID(fi.m.UID))
		if options.Flags {
			resp.WriteFlags(toImapFlags(fi.m.Flags))
		}
		if options.InternalDate {
			resp.WriteInternalDate(fi.m.InternalDate)
		}
		if options.RFC822Size {
			resp.WriteRFC822Size(fi.m.Size)
		}
		if options.Envelope {
			if hdr, herr := parseHeader(raw); herr == nil {
				resp.WriteEnvelope(imapserver.ExtractEnvelope(hdr))
			}
		}
		if options.BodyStructure != nil {
			resp.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(raw)))
		}
		for _, bs := range options.BodySection {
			buf := imapserver.ExtractBodySection(bytes.NewReader(raw), bs)
			wc := resp.WriteBodySection(bs, int64(len(buf)))
			_, werr := wc.Write(buf)
			cerr := wc.Close()
			if werr != nil {
				return werr
			}
			if cerr != nil {
				return cerr
			}
		}
		for _, bsec := range options.BinarySection {
			buf := imapserver.ExtractBinarySection(bytes.NewReader(raw), bsec)
			wc := resp.WriteBinarySection(bsec, int64(len(buf)))
			wc.Write(buf)
			wc.Close()
		}
		for _, bss := range options.BinarySectionSize {
			n := imapserver.ExtractBinarySectionSize(bytes.NewReader(raw), bss)
			resp.WriteBinarySectionSize(bss, n)
		}
		if err := resp.Close(); err != nil {
			return err
		}

		if markSeen && !hasFlag(fi.m.Flags, model.FlagSeen) {
			s.setFlagsUID(v, fi.m.UID, append(fi.m.Flags, model.FlagSeen), nil)
		}
	}
	return nil
}

// loadRaw fetches the raw RFC822 bytes for a message (blob store). Tenancy is
// enforced upstream: sessions only reach message ids from their own folders.
func (s *Server) loadRaw(ctx context.Context, messageID string) ([]byte, error) {
	uid, ok := parseID(messageID)
	if !ok {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "bad message id"}
	}
	full, err := store.GetMessage(ctx, s.st.Pool, uid)
	if err != nil {
		return nil, err
	}
	rc, err := s.blobs.Open(ctx, full.Message.BlobSHA256)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// ---- STORE ----

func (s *session) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	v, err := s.requireSelected()
	if err != nil {
		return err
	}
	var affected []uint32
	v.forEach(numSet, func(seq uint32, m msgInfo) {
		affected = append(affected, m.UID)
	})
	for _, uid := range affected {
		newFlags := s.applyStoreFlags(v, uid, flags)
		if newFlags == nil {
			continue
		}
	}
	if !flags.Silent {
		// Respond with fresh flags for the matched set.
		return s.Fetch(w, numSet, &imap.FetchOptions{Flags: true})
	}
	return nil
}

// applyStoreFlags mutates flags in the DB + snapshot; source=nil echoes the
// change to every session (the writer included) — safe because the writer's
// response is produced by its own Fetch when !silent.
func (s *session) applyStoreFlags(v *view, uid uint32, flags *imap.StoreFlags) []string {
	v.shared.mu.Lock()
	var current []string
	idx := -1
	for i, m := range v.shared.msgs {
		if m.UID == uid {
			current = m.Flags
			idx = i
			break
		}
	}
	v.shared.mu.Unlock()
	if idx < 0 {
		return nil
	}

	var next []string
	switch flags.Op {
	case imap.StoreFlagsSet:
		next = toStringFlags(flags.Flags)
	case imap.StoreFlagsAdd:
		next = append(append([]string{}, current...), toStringFlags(flags.Flags)...)
		next = dedupFlags(next)
	case imap.StoreFlagsDel:
		for _, f := range current {
			drop := false
			for _, df := range flags.Flags {
				if equalFold(f, string(df)) {
					drop = true
					break
				}
			}
			if !drop {
				next = append(next, f)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.srv.st.WithTx(ctx, func(tx store.Tx) error {
		return store.UpdateItemFlags(ctx, tx, v.shared.folder.ID, uid, next)
	})

	v.shared.mu.Lock()
	for i := range v.shared.msgs {
		if v.shared.msgs[i].UID == uid {
			v.shared.msgs[i].Flags = next
		}
	}
	seq := uint32(0)
	for i, m := range v.shared.msgs {
		if m.UID == uid {
			seq = uint32(i + 1)
		}
	}
	v.shared.mu.Unlock()

	if seq != 0 {
		v.shared.tracker.QueueMessageFlags(seq, imap.UID(uid), toImapFlags(next), v.st)
	}
	return next
}

func (s *session) setFlagsUID(v *view, uid uint32, flags []string, source *imapserver.SessionTracker) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.srv.st.WithTx(ctx, func(tx store.Tx) error {
		return store.UpdateItemFlags(ctx, tx, v.shared.folder.ID, uid, flags)
	})
	v.shared.mu.Lock()
	for i := range v.shared.msgs {
		if v.shared.msgs[i].UID == uid {
			v.shared.msgs[i].Flags = flags
		}
	}
	seq := uint32(0)
	for i, m := range v.shared.msgs {
		if m.UID == uid {
			seq = uint32(i + 1)
		}
	}
	v.shared.mu.Unlock()
	if seq != 0 {
		v.shared.tracker.QueueMessageFlags(seq, imap.UID(uid), toImapFlags(flags), source)
	}
}

func dedupFlags(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range in {
		k := lower(f)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, f)
	}
	return out
}

// ---- EXPUNGE ----
//
// The handler deletes from the DB and lets applyAndDiff queue the EXPUNGE
// updates; the framework flushes them to every session (this one included)
// via Poll before the tagged OK — never double-reported.
func (s *session) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	v, err := s.requireSelected()
	if err != nil {
		return err
	}
	_ = w
	v.shared.beginCommandDiff()
	defer v.shared.endCommandDiff()

	v.shared.mu.Lock()
	var victims []uint32
	for _, m := range v.shared.msgs {
		if !hasFlag(m.Flags, model.FlagDeleted) {
			continue
		}
		if uids != nil && !uids.Contains(imap.UID(m.UID)) {
			continue
		}
		victims = append(victims, m.UID)
	}
	v.shared.mu.Unlock()
	if len(victims) == 0 {
		return nil
	}

	for _, uid := range victims {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.srv.st.WithTx(ctx, func(tx store.Tx) error {
			_, err := store.ExpungeItem(ctx, tx, v.shared.folder.ID, uid)
			return err
		})
		cancel()
		if err != nil {
			continue
		}
	}
	_, err = v.shared.applyAndDiff()
	return err
}

// ---- SEARCH ----

func (s *session) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	v, err := s.requireSelected()
	if err != nil {
		return nil, err
	}
	data := &imap.SearchData{}
	var seqSet imap.SeqSet
	var uidSet imap.UIDSet

	v.shared.mu.Lock()
	type cand struct {
		seq uint32
		m   msgInfo
	}
	var all []cand
	for i, m := range v.shared.msgs {
		all = append(all, cand{seq: uint32(i + 1), m: m})
	}
	v.shared.mu.Unlock()

	for _, c := range all {
		ok, err := s.matchSearch(context.Background(), c.seq, c.m, criteria)
		if err != nil {
			continue
		}
		if !ok {
			continue
		}
		uidSet.AddNum(imap.UID(c.m.UID))
		var num uint32
		switch kind {
		case imapserver.NumKindSeq:
			enc := v.st.EncodeSeqNum(c.seq)
			if enc == 0 {
				continue
			}
			seqSet.AddNum(enc)
			num = enc
		case imapserver.NumKindUID:
			num = c.m.UID
		}
		if data.Min == 0 || num < data.Min {
			data.Min = num
		}
		if data.Max == 0 || num > data.Max {
			data.Max = num
		}
		data.Count++
	}
	switch kind {
	case imapserver.NumKindSeq:
		data.All = seqSet
	case imapserver.NumKindUID:
		data.All = uidSet
	}
	if options.ReturnSave {
		v.searchRes = uidSet
	}
	return data, nil
}

func (s *session) matchSearch(ctx context.Context, seqNum uint32, m msgInfo, criteria *imap.SearchCriteria) (bool, error) {
	for _, ss := range criteria.SeqNum {
		if !ss.Contains(seqNum) {
			return false, nil
		}
	}
	for _, us := range criteria.UID {
		if !us.Contains(imap.UID(m.UID)) {
			return false, nil
		}
	}
	if !matchDate(m.InternalDate, criteria.Since, criteria.Before) {
		return false, nil
	}
	for _, f := range criteria.Flag {
		if !hasFlag(m.Flags, string(f)) {
			return false, nil
		}
	}
	for _, f := range criteria.NotFlag {
		if hasFlag(m.Flags, string(f)) {
			return false, nil
		}
	}
	if criteria.Larger != 0 && m.Size <= criteria.Larger {
		return false, nil
	}
	if criteria.Smaller != 0 && m.Size >= criteria.Smaller {
		return false, nil
	}

	needRaw := len(criteria.Header) > 0 || len(criteria.Text) > 0 || len(criteria.Body) > 0 ||
		!criteria.SentBefore.IsZero() || !criteria.SentSince.IsZero()
	if needRaw {
		raw, err := s.srv.loadRaw(ctx, m.MessageID)
		if err != nil {
			return false, err
		}
		hdr, err := parseHeader(raw)
		if err != nil {
			return false, err
		}
		for _, hc := range criteria.Header {
			if !matchHeader(hdr, hc.Key, hc.Value) {
				return false, nil
			}
		}
		if !criteria.SentBefore.IsZero() || !criteria.SentSince.IsZero() {
			if d, derr := headerDate(hdr); derr == nil {
				if !matchDate(d, criteria.SentSince, criteria.SentBefore) {
					return false, nil
				}
			} else {
				return false, nil
			}
		}
		lowerRaw := lower(string(raw))
		for _, t := range criteria.Text {
			if t != "" && !bytes.Contains([]byte(lowerRaw), []byte(lower(t))) {
				return false, nil
			}
		}
		for _, b := range criteria.Body {
			if b != "" && !bytes.Contains([]byte(lowerRaw), []byte(lower(b))) {
				return false, nil
			}
		}
	}
	for _, not := range criteria.Not {
		ok, err := s.matchSearch(ctx, seqNum, m, &not)
		if err != nil || ok {
			return false, err
		}
	}
	for _, or := range criteria.Or {
		ok1, _ := s.matchSearch(ctx, seqNum, m, &or[0])
		ok2, _ := s.matchSearch(ctx, seqNum, m, &or[1])
		if !ok1 && !ok2 {
			return false, nil
		}
	}
	return true, nil
}

func matchDate(t time.Time, since, before time.Time) bool {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !before.IsZero() && !t.Before(before) {
		return false
	}
	return true
}

func matchHeader(hdr gotextproto.Header, key, pattern string) bool {
	vals := hdr.Values(key)
	if pattern == "" {
		return len(vals) > 0
	}
	for _, v := range vals {
		if bytes.Contains([]byte(lower(v)), []byte(lower(pattern))) {
			return true
		}
	}
	return false
}

// ---- COPY / MOVE ----

func (s *session) Copy(numSet imap.NumSet, destName string) (*imap.CopyData, error) {
	v, err := s.requireSelected()
	if err != nil {
		return nil, err
	}
	dest, err := s.resolveFolder(destName)
	if err != nil {
		return nil, err
	}
	if dest.ID == v.shared.folder.ID {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "source and destination are identical"}
	}
	ctx := context.Background()
	var srcUIDs, dstUIDs imap.UIDSet
	var ops []struct {
		folderID, uid string
	}
	v.forEach(numSet, func(seq uint32, m msgInfo) {
		ops = append(ops, struct {
			folderID, uid string
		}{v.shared.folder.ID.String(), m.MessageID})
	})
	_ = ops
	// Perform copies under the shared mutex to keep UID ordering stable.
	err = s.srv.st.WithTx(ctx, func(tx store.Tx) error {
		v.forEach(numSet, func(seq uint32, m msgInfo) {
			mid, _ := parseID(m.MessageID)
			newUID, cerr := store.CopyItem(ctx, tx, v.shared.folder.ID, dest.ID, m.UID)
			if cerr != nil {
				err = cerr
				return
			}
			_ = mid
			srcUIDs.AddNum(imap.UID(m.UID))
			dstUIDs.AddNum(imap.UID(newUID))
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if shared, ok := s.srv.getShared(dest.ID.String()); ok {
		_ = shared.reload()
	}
	return &imap.CopyData{UIDValidity: dest.UIDValidity, SourceUIDs: srcUIDs, DestUIDs: dstUIDs}, nil
}

// Move implements SessionMove (MOVE extension). Source expunges flow to all
// sessions through applyAndDiff; the framework flushes them via Poll before
// the tagged OK (RFC 6851-compliant, never double-reported).
func (s *session) Move(w *imapserver.MoveWriter, numSet imap.NumSet, destName string) error {
	v, err := s.requireSelected()
	if err != nil {
		return err
	}
	dest, err := s.resolveFolder(destName)
	if err != nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeTryCreate, Text: "No such mailbox"}
	}
	if dest.ID == v.shared.folder.ID {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "source and destination are identical"}
	}
	ctx := context.Background()

	v.shared.beginCommandDiff()
	defer v.shared.endCommandDiff()

	var srcUIDs, dstUIDs imap.UIDSet
	err = s.srv.st.WithTx(ctx, func(tx store.Tx) error {
		v.forEach(numSet, func(seq uint32, m msgInfo) {
			newUID, merr := store.MoveItem(ctx, tx, v.shared.folder.ID, dest.ID, m.UID)
			if merr != nil {
				err = merr
				return
			}
			srcUIDs.AddNum(imap.UID(m.UID))
			dstUIDs.AddNum(imap.UID(newUID))
		})
		return err
	})
	if err != nil {
		return err
	}

	if _, err := v.shared.applyAndDiff(); err != nil {
		return err
	}
	if shared, ok := s.srv.getShared(dest.ID.String()); ok {
		_, _ = shared.applyAndDiff()
	}
	return w.WriteCopyData(&imap.CopyData{UIDValidity: dest.UIDValidity, SourceUIDs: srcUIDs, DestUIDs: dstUIDs})
}

func (s *Server) getShared(folderID string) (*sharedFolder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sf, ok := s.shared[folderID]
	return sf, ok
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if 'A' <= b[i] && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func equalFold(a, b string) bool { return a == b || lower(a) == lower(b) }
