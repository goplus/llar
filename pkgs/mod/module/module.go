package module

type Version struct {
	ID      string
	Version string
}

type VersionComparator func(v1, v2 Version) int
