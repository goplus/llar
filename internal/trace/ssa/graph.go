package ssa

import "github.com/goplus/llar/internal/trace"

type Source uint8

const (
	SourceRecords Source = iota
	SourceEvents
)

func (source Source) String() string {
	switch source {
	case SourceEvents:
		return "events"
	default:
		return "records"
	}
}

type ActionKind uint8

const (
	KindGeneric ActionKind = iota
	KindCopy
	KindInstall
	KindConfigure
)

type PathRole uint8

const (
	RoleUnknown PathRole = iota
	RoleTooling
	RolePropagating
	RoleDelivery
)

func (kind ActionKind) String() string {
	switch kind {
	case KindCopy:
		return "copy"
	case KindInstall:
		return "install"
	case KindConfigure:
		return "configure"
	default:
		return "generic"
	}
}

func (role PathRole) String() string {
	switch role {
	case RoleTooling:
		return "tooling"
	case RolePropagating:
		return "propagating"
	case RoleDelivery:
		return "delivery"
	default:
		return "propagating"
	}
}

type ExecEdge struct {
	From int
	To   int
	Path string
}

type ExecNode struct {
	PID        int64
	ParentPID  int64
	Argv       []string
	Cwd        string
	Env        []string
	Reads      []string
	ReadMisses []string
	Writes     []string
	Deletes    []string
	ExecPath   string
	Tool       string
	Kind       ActionKind
	ActionKey  string
	// StructureKey and Fingerprint remain on the graph so passes can compare
	// intrinsic structure without reconstructing normalization context.
	StructureKey string
	Fingerprint  string
}

type PathInfo struct {
	Path    string
	Writers []int
	Readers []int
	Role    PathRole
}

type PathState struct {
	Writer    int
	Path      string
	Version   int
	Tombstone bool
	Missing   bool
}

type Read struct {
	Path string
	Defs []PathState
}

type observation struct {
	Nodes  []ExecNode
	Parent []int
	Paths  map[string]PathInfo
	Deps   [][]ExecEdge
}

type Graph struct {
	Source       Source
	Records      int
	Events       int
	Scope        trace.Scope
	InputDigests map[string]string
	RawRecords   []trace.Record
	RawEvents    []trace.Event

	Actions      []ExecNode
	ParentAction []int
	Out          [][]ExecEdge
	In           [][]ExecEdge
	Indeg        []int
	Outdeg       []int
	Tooling      []bool
	Probe        []bool
	Mainline     []bool
	RawPaths     map[string]PathInfo

	Nodes        []ExecNode
	Parent       []int
	Paths        map[string]PathInfo
	Deps         [][]ExecEdge
	ActionReads  [][]Read
	ActionWrites [][]PathState
	ReadersByDef map[PathState][]int
	InitialDefs  map[string]PathState
	DefsByPath   map[string][]PathState
}
