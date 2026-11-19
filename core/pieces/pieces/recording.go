// +gobra

package pieces

// MaxCapacity leaves room for the successor of a piece number on every Go
// architecture. This machine safety limit is independent of recording sizes.
const MaxCapacity = 1 << 30

// Piece is saved metadata, never media bytes. Numbers are its slice index + 1.
// A zero gap means that the piece was included in the joined file.
type Piece struct {
	Duration int
	Known    bool
	Omitted  bool
	Gap      int
}

// Metadata is the scalar part of the atomically persisted recording record.
// JoinCount describes the ordered request [1, ..., JoinCount]. No caller effects,
// crash history, or information obtained by inspecting a file lives here.
type Metadata struct {
	Phase            string
	Capture          string
	Open             int
	NextNumber       int
	FlushReported    bool
	CaptureFault     bool
	JoinCount        int
	Prepared         bool
	Interruptions    int
	HandedOver       bool
	RetentionElapsed bool
	Error            string
	ErrorAt          string
	RetryPending     bool
}

// Recording decides which caller operation is due. Save the entire record
// before executing the operation identified by Meta.Phase, and save each report
// before acting on its resulting phase. In particular, PublishJoin authorizes
// publication only after the omission manifest has been saved.
//
// Callers serialize access. They perform all I/O, report truthful results, and
// never replace a named file. A hand-over always refers to the joined file for
// ID; the recipient must deduplicate repeated requests by that stable identity.
// Pieces and Metadata are exposed for whole-record persistence, not arbitrary
// mutation. At launch, reconcile the restored record before running effects.
type Recording struct {
	Meta   Metadata
	Pieces []Piece
	ID     string
	Spool  string
	Limit  int
}

// New reserves metadata for capacity pieces. Durations are positive caller
// supplied units; gap durations use the same units. limit is the cumulative
// number of interrupted joins at which recovery reports failure.
// @ requires 0 <= capacity && capacity <= MaxCapacity
// @ requires 0 < limit && limit <= MaxCapacity
// @ ensures acc(&b.Meta) && acc(&b.Pieces) && acc(&b.ID) && acc(&b.Spool) && acc(&b.Limit)
// @ ensures forall i int :: { &b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> acc(&b.Pieces[i])
// @ ensures len(b.Pieces) == capacity && b.ID == id && b.Spool == spool && b.Limit == limit
// @ ensures b.Meta == Metadata{Phase: "Capture", Capture: "active", NextNumber: 1}
// @ ensures forall i int :: { b.Pieces[i] } 0 <= i && i < len(b.Pieces) ==> b.Pieces[i] == Piece{}
func New(capacity, limit int, id, spool string) (b *Recording) {
	return &Recording{
		Meta:   Metadata{Phase: "Capture", Capture: "active", NextNumber: 1},
		Pieces: make([]Piece, capacity), ID: id, Spool: spool, Limit: limit,
	}
}

// knownPrefix and namedPrefix describe cardinalities for the loop contracts.
// @ ghost
// @ requires 0 <= n && n <= len(ps) && len(ps) <= MaxCapacity
// @ ensures 0 <= result && result <= n
// @ decreases n
// @ pure func knownPrefix(ps seq[Piece], n int) (result int) { return n == 0 ? 0 : knownPrefix(ps, n-1) + (ps[n-1].Known ? 1 : 0) }

// @ ghost
// @ requires 0 <= n && n <= len(ns) && len(ns) <= MaxCapacity
// @ ensures 0 <= result && result <= n
// @ decreases n
// @ pure func namedPrefix(ns seq[bool], n int) (result int) { return n == 0 ? 0 : namedPrefix(ns, n-1) + (ns[n-1] ? 1 : 0) }

// @ requires len(ps) <= MaxCapacity
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i], 1/2)
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i], 1/2)
// @ ensures forall i int :: { ps[i] } 0 <= i && i < len(ps) ==> ps[i] == old(ps[i])
// @ ensures n == old(knownPrefix(seq(ps), len(ps))) && 0 <= n && n <= len(ps)
func countKnown(ps []Piece) (n int) {
	i := 0
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j], 1/2)
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < len(ps) ==> ps[j] == old(ps[j])
	// @ invariant seq(ps) == old(seq(ps))
	// @ invariant n == knownPrefix(seq(ps), i) && 0 <= n && n <= i
	// @ decreases len(ps) - i
	for i < len(ps) {
		if ps[i].Known {
			n++
		}
		i++
	}
	return n
}

// @ requires len(ps) <= MaxCapacity
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { ps[i] } 0 <= i && i < len(ps) ==> ps[i].Duration == old(ps[i].Duration) && ps[i].Known == old(ps[i].Known) && !ps[i].Omitted && ps[i].Gap == 0
func clearManifest(ps []Piece) {
	i := 0
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j])
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < len(ps) ==> ps[j].Duration == old(ps[j].Duration) && ps[j].Known == old(ps[j].Known)
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < i ==> !ps[j].Omitted && ps[j].Gap == 0
	// @ decreases len(ps) - i
	for i < len(ps) {
		ps[i].Omitted = false
		ps[i].Gap = 0
		i++
	}
}

// @ requires len(ps) <= MaxCapacity && len(missing) == len(ps)
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i], 1/2)
// @ requires forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i], 1/2)
// @ ensures forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { ps[i] } 0 <= i && i < len(ps) ==> ps[i] == old(ps[i]) && missing[i] == old(missing[i])
// @ ensures valid == ((forall i int :: 0 <= i && i < len(ps) ==> (missing[i] ==> ps[i].Known)) && (exists i int :: 0 <= i && i < len(ps) && ps[i].Known && !missing[i]))
func validManifest(ps []Piece, missing []bool) (valid bool) {
	readable := false
	i := 0
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(missing) == len(ps)
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j], 1/2)
	// @ invariant forall j int :: { &missing[j] } 0 <= j && j < len(missing) ==> acc(&missing[j], 1/2)
	// @ invariant forall j int :: { ps[j] } 0 <= j && j < len(ps) ==> ps[j] == old(ps[j]) && missing[j] == old(missing[j])
	// @ invariant forall j int :: 0 <= j && j < i ==> (missing[j] ==> ps[j].Known)
	// @ invariant readable == (exists j int :: 0 <= j && j < i && ps[j].Known && !missing[j])
	// @ decreases len(ps) - i
	for i < len(ps) {
		if missing[i] && !ps[i].Known {
			return false
		}
		if ps[i].Known && !missing[i] {
			readable = true
		}
		i++
	}
	return readable
}
