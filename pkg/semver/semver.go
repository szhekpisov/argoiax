package semver

import (
	"sort"

	sv "github.com/Masterminds/semver/v3"
)

// LatestStable returns the highest non-prerelease version, optionally constrained.
func LatestStable(versions []string, constraint string) (string, error) {
	var c *sv.Constraints
	if constraint != "" {
		var err error
		c, err = sv.NewConstraint(constraint)
		if err != nil {
			return "", err
		}
	}

	parsed := make([]*sv.Version, 0, len(versions))
	for _, v := range versions {
		ver, err := sv.NewVersion(v)
		if err != nil {
			continue
		}
		if ver.Prerelease() != "" {
			continue
		}
		if c != nil && !c.Check(ver) {
			continue
		}
		parsed = append(parsed, ver)
	}

	if len(parsed) == 0 {
		return "", nil
	}

	sort.Sort(sv.Collection(parsed))
	return parsed[len(parsed)-1].Original(), nil
}

// parsePair parses two version strings and returns both parsed versions.
func parsePair(current, latest string) (cur, lat *sv.Version, err error) {
	cur, err = sv.NewVersion(current)
	if err != nil {
		return nil, nil, err
	}
	lat, err = sv.NewVersion(latest)
	if err != nil {
		return nil, nil, err
	}
	return cur, lat, nil
}

// IsMajorBump returns true if the latest version has a higher major version than current.
func IsMajorBump(current, latest string) bool {
	cur, lat, err := parsePair(current, latest)
	if err != nil {
		return false
	}
	return lat.Major() > cur.Major()
}

// VersionsBetween returns all versions between (exclusive) current and latest, sorted ascending.
func VersionsBetween(versions []string, current, latest string) []string {
	cur, lat, err := parsePair(current, latest)
	if err != nil {
		return nil
	}

	var between []*sv.Version
	for _, v := range versions {
		ver, err := sv.NewVersion(v)
		if err != nil {
			continue
		}
		if ver.GreaterThan(cur) && ver.LessThan(lat) {
			between = append(between, ver)
		}
	}

	sort.Sort(sv.Collection(between))
	result := make([]string, len(between))
	for i, v := range between {
		result[i] = v.Original()
	}
	return result
}
