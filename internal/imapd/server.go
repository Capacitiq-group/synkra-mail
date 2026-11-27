// Package imapd: the customer-facing IMAP service (Section 6.2, day-1 scope).
// go-imap v2 handles the wire protocol; our backend enforces tenancy, app-password
// auth, and Postgres-backed state. IDLE works through a shared per-folder tracker
// fed by Postgres NOTIFY (no polling loops).
package imapd

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/events"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func newID() uuid.UUID { return uuid.New() }

const delimiter = '/'

type Server struct {
	opts  imapserver.Options
	cfg   *config.Config
	st    *store.Store
	auth  *authn.Service
	blobs blob.Store
	hub   *events.Hub

	mu     sync.Mutex
	shared map[string]*sharedFolder // folderID → shared state (trackers + snapshot)
}

func New(cfg *config.Config, st *store.Store, auth *authn.Service, blobs blob.Store, hub *events.Hub) *Server {
	s := &Server{cfg: cfg, st: st, auth: auth, blobs: blobs, hub: hub, shared: map[string]*sharedFolder{}}
	s.opts = imapserver.Options{
		NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{srv: s}, nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1:    {},
			imap.CapIdle:         {},
			imap.CapUIDPlus:      {},
			imap.CapUnselect:     {},
			imap.CapMove:         {},
			imap.CapESearch:      {},
			imap.CapNamespace:    {},
			imap.CapListExtended: {},
			imap.CapListStatus:   {},
			imap.CapStatusSize:   {},
			imap.CapSpecialUse:   {},
			imap.CapChildren:     {},
		},
		InsecureAuth: cfg.Env != "prod", // prod: TLS required before AUTH (Section 6.1)
	}
	if cfg.TLSEnabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			panic(fmt.Sprintf("imap tls: %v", err))
		}
		s.opts.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	return s
}

func (s *Server) ListenAndServe() error {
	srv := imapserver.New(&s.opts)
	return srv.ListenAndServe(s.cfg.IMAPAddr)
}

// ServeListener runs the IMAP server on an existing listener (tests, socket handover).
func (s *Server) ServeListener(ln net.Listener) error {
	srv := imapserver.New(&s.opts)
	return srv.Serve(ln)
}

// ---- IMAP session state ----

type session struct {
	srv     *Server
	user    *model.User
	mailbox *model.Mailbox // the user's own mailbox (v1 IMAP scope)
	view    *view
}

// view is one session's selected-folder state.
type view struct {
	sess      *session
	shared    *sharedFolder
	st        *imapserver.SessionTracker
	name      string
	searchRes imap.UIDSet // SEARCHRES ($)
}

var (
	_ imapserver.Session          = (*session)(nil)
	_ imapserver.SessionMove      = (*session)(nil)
	_ imapserver.SessionNamespace = (*session)(nil)
)

func (s *session) Close() error {
	if s.view != nil {
		s.view.close()
		s.view = nil
	}
	return nil
}

func (s *session) Login(username, password string) error {
	// go-imap v2 beta.8 exposes no remote address to sessions, so the
	// failed-attempt lockout is keyed by the login identity here (register §1.6).
	u, _, err := s.srv.auth.AuthenticateAppPassword(context.Background(), username, password, "")
	if err != nil {
		if errors.Is(err, authn.ErrLocked) {
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "account locked: too many failed attempts"}
		}
		return imapserver.ErrAuthFailed
	}
	mailboxes, err := store.MailboxesForUser(context.Background(), s.srv.st.Pool, u.ID)
	if err != nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "backend error"}
	}
	var own *model.Mailbox
	for i := range mailboxes {
		mb := &mailboxes[i]
		if mb.Kind == "user" && mb.OwnerUserID != nil && *mb.OwnerUserID == u.ID {
			own = mb
			break
		}
	}
	if own == nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "no mailbox provisioned"}
	}
	s.user = u
	s.mailbox = own
	return nil
}

func (s *session) requireAuth() error {
	if s.user == nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed, Text: "not authenticated"}
	}
	return nil
}

// resolveFolder maps an IMAP mailbox name to a folder (INBOX case-insensitive).
func (s *session) resolveFolder(name string) (*model.Folder, error) {
	folders, err := store.ListFolders(context.Background(), s.srv.st.Pool, s.mailbox.ID)
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if strings.EqualFold(folders[i].Name, name) {
			return &folders[i], nil
		}
	}
	return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNonExistent, Text: "No such mailbox"}
}

func (s *session) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return nil, err
	}
	if s.view != nil {
		s.view.close()
		s.view = nil
	}
	shared := s.srv.joinShared(folder)
	if err := shared.reload(); err != nil {
		s.srv.leaveShared(folder.ID.String())
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "backend error"}
	}
	v := &view{sess: s, shared: shared, st: shared.tracker.NewSession(), name: folder.Name}
	s.view = v
	return shared.selectData(), nil
}

func (s *session) Create(name string, options *imap.CreateOptions) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	name = strings.TrimRight(name, string(delimiter))
	if strings.EqualFold(name, "INBOX") {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "INBOX already exists"}
	}
	f := &model.Folder{ID: newID(), MailboxID: s.mailbox.ID, Name: name}
	return s.srv.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.CreateFolder(context.Background(), tx, f)
	})
}

func (s *session) Delete(name string) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return err
	}
	return s.srv.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.DeleteFolder(context.Background(), tx, folder.ID)
	})
}

func (s *session) Rename(name, newName string, options *imap.RenameOptions) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return err
	}
	return s.srv.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.RenameFolder(context.Background(), tx, folder.ID, newName)
	})
}

func (s *session) Subscribe(name string) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return err
	}
	return s.srv.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.SetFolderSubscribed(context.Background(), tx, folder.ID, true)
	})
}

func (s *session) Unsubscribe(name string) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return err
	}
	return s.srv.st.WithTx(context.Background(), func(tx store.Tx) error {
		return store.SetFolderSubscribed(context.Background(), tx, folder.ID, false)
	})
}

func (s *session) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	folders, err := store.ListFolders(context.Background(), s.srv.st.Pool, s.mailbox.ID)
	if err != nil {
		return err
	}
	if len(patterns) == 0 {
		patterns = []string{"*"}
	}
	for i := range folders {
		f := &folders[i]
		matched := false
		for _, p := range patterns {
			if imapserver.MatchList(f.Name, delimiter, ref, p) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if options.SelectSubscribed && !f.Subscribed {
			continue
		}
		data := &imap.ListData{Mailbox: f.Name, Delim: delimiter}
		if f.Subscribed {
			data.Attrs = append(data.Attrs, imap.MailboxAttrSubscribed)
		}
		switch f.SpecialUse {
		case model.FolderInbox:
			data.Attrs = append(data.Attrs, imap.MailboxAttr("\\Inbox"))
		case model.FolderSent:
			data.Attrs = append(data.Attrs, imap.MailboxAttrSent)
		case model.FolderDrafts:
			data.Attrs = append(data.Attrs, imap.MailboxAttrDrafts)
		case model.FolderTrash:
			data.Attrs = append(data.Attrs, imap.MailboxAttrTrash)
		case model.FolderJunk:
			data.Attrs = append(data.Attrs, imap.MailboxAttrJunk)
		case model.FolderArchive:
			data.Attrs = append(data.Attrs, imap.MailboxAttrArchive)
		}
		if options.ReturnStatus != nil {
			st, err := s.statusFor(f, options.ReturnStatus)
			if err != nil {
				return err
			}
			data.Status = st
		}
		if err := w.WriteList(data); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return nil, err
	}
	return s.statusFor(folder, options)
}

func (s *session) statusFor(f *model.Folder, options *imap.StatusOptions) (*imap.StatusData, error) {
	ctx := context.Background()
	// Refresh counters from the folder row (trigger-maintained).
	fresh, err := store.GetFolder(ctx, s.srv.st.Pool, f.ID)
	if err != nil {
		return nil, err
	}
	data := &imap.StatusData{Mailbox: f.Name}
	if options.NumMessages {
		n := fresh.TotalMessages
		data.NumMessages = &n
	}
	if options.UIDNext {
		data.UIDNext = imap.UID(fresh.UIDNext)
	}
	if options.UIDValidity {
		data.UIDValidity = fresh.UIDValidity
	}
	if options.NumUnseen {
		n := fresh.Unread
		data.NumUnseen = &n
	}
	if options.Size {
		var total int64
		if err := s.srv.st.Pool.QueryRow(ctx, `
                        select coalesce(sum(m.size_bytes),0) from mail.mailbox_item i
                        join mail.message m on m.id = i.message_id
                        where i.folder_id=$1 and i.deleted_at is null`, f.ID).Scan(&total); err != nil {
			return nil, err
		}
		data.Size = &total
	}
	return data, nil
}

func (s *session) Append(name string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	folder, err := s.resolveFolder(name)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeTryCreate, Text: "No such mailbox"}
	}
	raw, err := readLimited(r, s.srv.cfg.MaxMessageBytes)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "message too large"}
	}
	item, uid, err := s.srv.appendMessage(context.Background(), folder, raw, options.Flags, options.Time)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "append failed"}
	}
	_ = item
	// Apply the change to the shared tracker synchronously (guarding against
	// the NOTIFY listener double-firing) so EXISTS reaches this session and
	// any other viewers promptly.
	if shared, ok := s.srv.getShared(folder.ID.String()); ok {
		shared.beginCommandDiff()
		_, _ = shared.applyAndDiff()
		shared.endCommandDiff()
	}
	return &imap.AppendData{UIDValidity: folder.UIDValidity, UID: imap.UID(uid)}, nil
}

func (s *session) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	if s.view == nil {
		return nil
	}
	return s.view.st.Poll(w, allowExpunge)
}

func (s *session) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if s.view == nil {
		return nil
	}
	return s.view.st.Idle(w, stop)
}

func (s *session) Unselect() error {
	if s.view != nil {
		s.view.close()
		s.view = nil
	}
	return nil
}

func (s *session) Namespace() (*imap.NamespaceData, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return &imap.NamespaceData{Personal: []imap.NamespaceDescriptor{{Prefix: "", Delim: delimiter}}}, nil
}

// ---- shared folder state (tracker + snapshot + event listener) ----

type msgInfo struct {
	UID          uint32
	Flags        []string
	MessageID    string
	Size         int64
	InternalDate time.Time
}

type sharedFolder struct {
	srv    *Server
	folder model.Folder

	mu       sync.Mutex
	msgs     []msgInfo // ascending UID snapshot
	refs     int
	sub      *events.Subscriber
	stopCh   chan struct{}
	tracker  *imapserver.MailboxTracker
	suppress int // >0 while a command handler applies its own diff (blocks reload double-fire)
}

// joinShared returns (creating if needed) the shared state for a folder.
func (s *Server) joinShared(folder *model.Folder) *sharedFolder {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := folder.ID.String()
	sf, ok := s.shared[key]
	if ok {
		sf.refs++
		return sf
	}
	sf = &sharedFolder{
		srv: s, folder: *folder, refs: 1,
		stopCh:  make(chan struct{}),
		tracker: imapserver.NewMailboxTracker(0),
	}
	sf.sub = s.hub.Subscribe(folder.MailboxID.String())
	s.shared[key] = sf
	go sf.listen()
	return sf
}

func (s *Server) leaveShared(folderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sf, ok := s.shared[folderID]; ok {
		sf.refs--
		if sf.refs <= 0 {
			close(sf.stopCh)
			s.hub.Unsubscribe(sf.sub)
			delete(s.shared, folderID)
		}
	}
}

// listen applies NOTIFY events by diffing the DB snapshot into the tracker.
func (sf *sharedFolder) listen() {
	for {
		select {
		case <-sf.stopCh:
			return
		case ch, ok := <-sf.sub.C():
			if !ok {
				return
			}
			if ch.FolderID != sf.folder.ID.String() {
				continue
			}
			if err := sf.reload(); err != nil {
				continue
			}
		}
	}
}

// beginCommandDiff marks a command handler as the authoritative applier for
// changes it is about to commit, so the NOTIFY-triggered reload cannot
// double-apply them to the tracker.
func (sf *sharedFolder) beginCommandDiff() {
	sf.mu.Lock()
	sf.suppress++
	sf.mu.Unlock()
}

func (sf *sharedFolder) endCommandDiff() {
	sf.mu.Lock()
	sf.suppress--
	sf.mu.Unlock()
}

// applyAndDiff syncs the snapshot from the DB and queues the differences into
// the shared tracker (EXISTS/EXPUNGE/FLAGS). Returns the expunged canonical
// sequence numbers (descending) so command handlers can write per-session
// responses (e.g. MOVE). Safe: serialized against reload by mu + suppress.
func (sf *sharedFolder) applyAndDiff() (expungedSeqs []uint32, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, err := store.ListItems(ctx, sf.srv.st.Pool, sf.folder.ID, 0, 0xffffffff)
	if err != nil {
		return nil, err
	}
	fresh, err := store.GetFolder(ctx, sf.srv.st.Pool, sf.folder.ID)
	if err != nil {
		return nil, err
	}

	sf.mu.Lock()
	sf.folder.UIDNext = fresh.UIDNext
	sf.folder.TotalMessages = fresh.TotalMessages
	sf.folder.Unread = fresh.Unread
	old := sf.msgs
	sf.msgs = make([]msgInfo, 0, len(items))
	for _, it := range items {
		sf.msgs = append(sf.msgs, msgInfo{
			UID: it.Item.UID, Flags: it.Item.Flags,
			MessageID: it.Message.ID.String(), Size: it.Message.SizeBytes,
			InternalDate: it.Message.InternalDate,
		})
	}
	newMsgs := append([]msgInfo(nil), sf.msgs...)
	sf.mu.Unlock()

	// EXISTS only ever grows via QueueNumMessages (the tracker panics on a
	// decreasing count — removals are expressed as EXPUNGE updates below).
	if len(newMsgs) > len(old) {
		sf.tracker.QueueNumMessages(uint32(len(newMsgs)))
	}

	// Diff removed: expunge (descending seq numbers).
	newSet := map[uint32]struct{}{}
	for _, m := range newMsgs {
		newSet[m.UID] = struct{}{}
	}
	for i := len(old) - 1; i >= 0; i-- {
		if _, ok := newSet[old[i].UID]; !ok {
			seq := uint32(i + 1)
			expungedSeqs = append(expungedSeqs, seq)
			sf.tracker.QueueExpunge(seq)
		}
	}

	// Diff flag changes.
	newFlagMap := map[uint32][]string{}
	for _, m := range newMsgs {
		newFlagMap[m.UID] = m.Flags
	}
	for i, m := range old {
		nf, ok := newFlagMap[m.UID]
		if !ok || !flagsEqual(nf, m.Flags) {
			sf.tracker.QueueMessageFlags(uint32(i+1), imap.UID(m.UID), toImapFlags(nf), nil)
		}
	}
	return expungedSeqs, nil
}

// reload diffs the DB state into the tracker (NOTIFY listener path).
func (sf *sharedFolder) reload() error {
	sf.mu.Lock()
	suppressed := sf.suppress > 0
	sf.mu.Unlock()
	if suppressed {
		return nil // a command handler owns this change
	}
	_, err := sf.applyAndDiff()
	return err
}

func (sf *sharedFolder) selectData() *imap.SelectData {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	flags := []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft}
	permanent := append(append([]imap.Flag{}, flags...), imap.FlagWildcard)
	firstUnseen := uint32(0)
	for i, m := range sf.msgs {
		if !hasFlag(m.Flags, model.FlagSeen) {
			firstUnseen = uint32(i + 1)
			break
		}
	}
	return &imap.SelectData{
		Flags:             flags,
		PermanentFlags:    permanent,
		NumMessages:       uint32(len(sf.msgs)),
		FirstUnseenSeqNum: firstUnseen,
		UIDNext:           imap.UID(sf.folder.UIDNext),
		UIDValidity:       sf.folder.UIDValidity,
	}
}

// withView applies fn to each message matched by numSet, in sequence order.
func (v *view) forEach(numSet imap.NumSet, fn func(seqNum uint32, m msgInfo)) {
	v.shared.mu.Lock()
	defer v.shared.mu.Unlock()
	max := uint32(len(v.shared.msgs))
	numSet = staticNumSet(numSet, max, uint32(v.shared.folder.UIDNext)-1)
	for i, m := range v.shared.msgs {
		seqNum := uint32(i + 1)
		var contains bool
		switch ns := numSet.(type) {
		case imap.SeqSet:
			enc := v.st.EncodeSeqNum(seqNum)
			contains = enc != 0 && ns.Contains(enc)
		case imap.UIDSet:
			contains = ns.Contains(imap.UID(m.UID))
		}
		if contains {
			fn(seqNum, m)
		}
	}
}

// staticNumSet resolves "*" against current counts (memserver semantics).
func staticNumSet(numSet imap.NumSet, maxSeq, maxUID uint32) imap.NumSet {
	switch ns := numSet.(type) {
	case imap.SeqSet:
		for i := range ns {
			fixRange(&ns[i].Start, &ns[i].Stop, maxSeq)
		}
	case imap.UIDSet:
		for i := range ns {
			fixRange((*uint32)(&ns[i].Start), (*uint32)(&ns[i].Stop), maxUID)
		}
	}
	return numSet
}

func fixRange(start, stop *uint32, max uint32) {
	dyn := false
	if *start == 0 {
		*start = max
		dyn = true
	}
	if *stop == 0 {
		*stop = max
		dyn = true
	}
	if dyn && *start > *stop {
		*start, *stop = *stop, *start
	}
}

func (v *view) close() {
	v.st.Close()
	v.shared.srv.leaveShared(v.shared.folder.ID.String())
}

func flagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]struct{}{}
	for _, f := range a {
		am[f] = struct{}{}
	}
	for _, f := range b {
		if _, ok := am[f]; !ok {
			return false
		}
	}
	return true
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

func toImapFlags(flags []string) []imap.Flag {
	out := make([]imap.Flag, 0, len(flags))
	for _, f := range flags {
		out = append(out, imap.Flag(f))
	}
	return out
}

func readLimited(r imap.LiteralReader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(r, limit)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// appendMessage stores a client-uploaded message (IMAP APPEND). Raw bytes are
// stored verbatim; metadata is parsed best-effort for threads/search.
func (s *Server) appendMessage(ctx context.Context, folder *model.Folder, raw []byte, flags []imap.Flag, t time.Time) (string, uint32, error) {
	parsed, _ := mimeutil.Parse(raw) // best effort
	sha, size, err := s.blobs.Put(ctx, bytes.NewReader(raw))
	if err != nil {
		return "", 0, err
	}
	mb, err := store.GetMailbox(ctx, s.st.Pool, folder.MailboxID)
	if err != nil {
		return "", 0, err
	}
	msg := &model.Message{
		ID: newID(), OrgID: mb.OrgID, MailboxID: mb.ID, BlobSHA256: sha,
		Direction: "internal", MessageID: parsed.MessageID, InReplyTo: parsed.InReplyTo,
		Refs: parsed.Refs, FromEmail: parsed.FromEmail, FromName: parsed.FromName,
		ToAddrs: toModelEmailAddrs(parsed.To), CcAddrs: toModelEmailAddrs(parsed.Cc),
		Subject: parsed.Subject, Snippet: mimeutil.Snippet(parsed.Text), BodyText: parsed.Text,
		HasAttachments: len(parsed.Attachments) > 0, SizeBytes: size,
		InternalDate: pickTime(t), CreatedAt: time.Now(),
	}
	var uid uint32
	err = s.st.WithTx(ctx, func(tx store.Tx) error {
		subjNorm := mimeutil.NormalizeSubject(parsed.Subject)
		threadID, terr := store.FindOrCreateThread(ctx, tx, mb.OrgID, parsed.Refs, parsed.InReplyTo, subjNorm)
		if terr != nil {
			return terr
		}
		msg.ThreadID = &threadID
		_, uid, err = store.InsertMessage(ctx, tx, msg, folder.ID, toStringFlags(flags))
		return err
	})
	if err != nil {
		return "", 0, err
	}
	// The originating session's Select snapshot refreshes on next Poll/Idle via
	// the NOTIFY listener; but APPEND from the *same* session must reflect
	// immediately — the reload diff covers all sessions uniformly.
	return msg.ID.String(), uid, nil
}

func pickTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func toModelEmailAddrs(l []mimeutil.Addr) []model.EmailAddr {
	out := make([]model.EmailAddr, 0, len(l))
	for _, a := range l {
		out = append(out, model.EmailAddr{Name: a.Name, Email: a.Email})
	}
	return out
}

func toStringFlags(flags []imap.Flag) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		out = append(out, string(f))
	}
	return out
}

func sortUIDs(uids []uint32) {
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
}
