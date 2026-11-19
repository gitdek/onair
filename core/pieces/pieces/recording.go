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
// @ pure func knownPrefix(ps seq[Piece], n int) (result int) { return len(knownIndexes(ps, n)) }

// knownIndexes lists the indexes of the known pieces among the first n, in
// order. Gobra's overflow check bounds every integer a function's body
// computes, whichever branch it is in, so a recursive sum would be checked
// at n == 0 too. A list of indexes needs no sum, and its length is the count.
// @ ghost
// @ requires 0 <= n && n <= len(ps)
// @ ensures len(result) <= n
// @ decreases n
// @ pure func knownIndexes(ps seq[Piece], n int) (result seq[int]) { return n == 0 ? seq[int]{} : knownIndexes(ps, n-1) ++ (ps[n-1].Known ? seq[int]{n - 1} : seq[int]{}) }

// @ ghost
// @ requires 0 <= n && n <= len(ns) && len(ns) <= MaxCapacity
// @ ensures 0 <= result && result <= n
// @ decreases n
// @ pure func namedPrefix(ns seq[bool], n int) (result int) { return len(namedIndexes(ns, n)) }

// namedIndexes lists the indexes of the named pieces among the first n, in
// order, for the reason knownIndexes does.
// @ ghost
// @ requires 0 <= n && n <= len(ns)
// @ ensures len(result) <= n
// @ decreases n
// @ pure func namedIndexes(ns seq[bool], n int) (result seq[int]) { return n == 0 ? seq[int]{} : namedIndexes(ns, n-1) ++ (ns[n-1] ? seq[int]{n - 1} : seq[int]{}) }

// The helpers below take the pieces whole, and give them back with what is
// true of each piece in the quantifier that holds its permission, triggered
// on its address. Facts in quantifiers of their own, triggered on a piece's
// value, and loops that restated the heap as a sequence, ran past Gobra's
// time limit, in these helpers and in the methods that call them.

// countKnown is the number of known pieces. It counts over a ghost copy of
// the pieces, so the loop states nothing about the heap but each piece.
// @ requires len(ps) <= MaxCapacity
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && ps[i] == old(ps[i])
// @ ensures n == old(knownPrefix(seq(ps), len(ps))) && 0 <= n && n <= len(ps)
func countKnown(ps []Piece) (n int) {
	// @ ghost s := seq(ps)
	i := 0
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j] == old(ps[j])
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(s) == len(ps)
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> s[j] == ps[j]
	// @ invariant n == knownPrefix(s, i) && 0 <= n && n <= i
	// @ decreases len(ps) - i
	for i < len(ps) {
		if ps[i].Known {
			n++
		}
		i++
	}
	return n
}

// clearManifest forgets a join's omissions: every piece keeps its duration
// and whether it is known, and none is omitted or has a gap.
// @ requires len(ps) <= MaxCapacity
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && ps[i].Duration == old(ps[i].Duration) && ps[i].Known == old(ps[i].Known) && !ps[i].Omitted && ps[i].Gap == 0
func clearManifest(ps []Piece) {
	i := 0
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j].Duration == old(ps[j].Duration) && ps[j].Known == old(ps[j].Known) && (j < i ==> !ps[j].Omitted && ps[j].Gap == 0)
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity
	// @ decreases len(ps) - i
	for i < len(ps) {
		ps[i].Omitted = false
		ps[i].Gap = 0
		i++
	}
}

// omittedUnknown is the index of a piece the join left out though it isn't
// known, or -1 when every piece it left out is known.
// @ requires len(ps) <= MaxCapacity && len(missing) == len(ps)
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ requires forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2) && missing[i] == old(missing[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && ps[i] == old(ps[i])
// @ ensures -1 <= k && k < len(ps)
// @ ensures 0 <= k ==> missing[k] && !ps[k].Known
// @ ensures k < 0 ==> forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> (missing[i] ==> ps[i].Known)
func omittedUnknown(ps []Piece, missing []bool) (k int) {
	i := 0
	// @ invariant forall j int :: { &missing[j] } 0 <= j && j < len(missing) ==> acc(&missing[j], 1/2) && missing[j] == old(missing[j])
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j] == old(ps[j])
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(missing) == len(ps)
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < i ==> (missing[j] ==> ps[j].Known)
	// @ decreases len(ps) - i
	for i < len(ps) {
		if missing[i] && !ps[i].Known {
			return i
		}
		i++
	}
	return -1
}

// includedKnown is the index of a known piece the join didn't leave out, or
// -1 when it left out every known piece.
// @ requires len(ps) <= MaxCapacity && len(missing) == len(ps)
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ requires forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2) && missing[i] == old(missing[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && ps[i] == old(ps[i])
// @ ensures -1 <= k && k < len(ps)
// @ ensures 0 <= k ==> ps[k].Known && !missing[k]
// @ ensures k < 0 ==> forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> !(ps[i].Known && !missing[i])
func includedKnown(ps []Piece, missing []bool) (k int) {
	i := 0
	// @ invariant forall j int :: { &missing[j] } 0 <= j && j < len(missing) ==> acc(&missing[j], 1/2) && missing[j] == old(missing[j])
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j] == old(ps[j])
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(missing) == len(ps)
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < i ==> !(ps[j].Known && !missing[j])
	// @ decreases len(ps) - i
	for i < len(ps) {
		if ps[i].Known && !missing[i] {
			return i
		}
		i++
	}
	return -1
}

// fillManifest records which pieces the join left out: each is omitted, and
// its gap is its duration. Every other piece is included, with no gap.
// @ requires len(ps) <= MaxCapacity && len(missing) == len(ps)
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ requires forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2)
// @ ensures forall i int :: { &missing[i] } 0 <= i && i < len(missing) ==> acc(&missing[i], 1/2) && missing[i] == old(missing[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && ps[i].Known == old(ps[i].Known) && ps[i].Duration == old(ps[i].Duration) && ps[i].Omitted == missing[i] && ps[i].Gap == (missing[i] ? old(ps[i].Duration) : 0)
func fillManifest(ps []Piece, missing []bool) {
	i := 0
	// @ invariant forall j int :: { &missing[j] } 0 <= j && j < len(missing) ==> acc(&missing[j], 1/2) && missing[j] == old(missing[j])
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j].Known == old(ps[j].Known) && ps[j].Duration == old(ps[j].Duration) && (j < i ==> ps[j].Omitted == missing[j] && ps[j].Gap == (missing[j] ? ps[j].Duration : 0)) && (i <= j ==> ps[j] == old(ps[j]))
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity && len(missing) == len(ps)
	// @ decreases len(ps) - i
	for i < len(ps) {
		ps[i].Omitted = missing[i]
		ps[i].Gap = 0
		if missing[i] {
			ps[i].Gap = ps[i].Duration
		}
		i++
	}
}

// forgetPieces records that no piece is on disk any more. Each keeps its
// duration and its manifest entry.
// @ requires len(ps) <= MaxCapacity
// @ requires forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i])
// @ ensures forall i int :: { &ps[i] } 0 <= i && i < len(ps) ==> acc(&ps[i]) && !ps[i].Known && ps[i].Duration == old(ps[i].Duration) && ps[i].Omitted == old(ps[i].Omitted) && ps[i].Gap == old(ps[i].Gap)
func forgetPieces(ps []Piece) {
	i := 0
	// @ invariant forall j int :: { &ps[j] } 0 <= j && j < len(ps) ==> acc(&ps[j]) && ps[j].Duration == old(ps[j].Duration) && ps[j].Omitted == old(ps[j].Omitted) && ps[j].Gap == old(ps[j].Gap) && (j < i ==> !ps[j].Known)
	// @ invariant 0 <= i && i <= len(ps) && len(ps) <= MaxCapacity
	// @ decreases len(ps) - i
	for i < len(ps) {
		ps[i].Known = false
		i++
	}
}
