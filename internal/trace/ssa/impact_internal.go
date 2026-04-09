package ssa

type pathStateKey struct {
	path      string
	tombstone bool
	missing   bool
}

type optionProfile struct {
	seedWrites map[string]struct{}
	needPaths  map[string]struct{}
	slicePaths map[string]struct{}
	joinSet    []int
	seedStates map[pathStateKey]struct{}
	needStates map[pathStateKey]struct{}
	flowStates map[pathStateKey]struct{}
	ambiguous  bool
}

type actionPair struct {
	baseIdx  int
	probeIdx int
}

type impactEvidence struct {
	changed map[string]bool
}

type pathSSAFlow struct {
	reachedDefs     map[PathState]struct{}
	reachedActions  map[int]struct{}
	joinActions     []int
	flowActions     []int
	frontierActions []int
	externalReads   map[int]map[string]struct{}
	externalDefs    map[int]map[PathState]struct{}
	ambiguousReads  bool
}

type deletedSeedSet map[string]struct{}

func initOptionProfile() optionProfile {
	return optionProfile{
		seedWrites: make(map[string]struct{}),
		needPaths:  make(map[string]struct{}),
		slicePaths: make(map[string]struct{}),
		seedStates: make(map[pathStateKey]struct{}),
		needStates: make(map[pathStateKey]struct{}),
		flowStates: make(map[pathStateKey]struct{}),
	}
}

func canonicalImpactPath(graph Graph, path string) string {
	return normalizeScopeToken(path, graph.Scope)
}

func pathChanged(evidence *impactEvidence, graph Graph, path string) bool {
	if evidence == nil {
		return true
	}
	key := canonicalImpactPath(graph, path)
	changed, ok := evidence.changed[key]
	if !ok {
		return true
	}
	return changed
}
