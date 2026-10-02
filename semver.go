package main

// Versions and version ranges the way VPM repositories write them (npm style): "3.10.5", "3.1.x",
// "^1.14", "~3.2.0", ">=1.8.0 <2.0.0-a", ">=3.10.4 < 3.11.X", "3.2 - 3.10", "3.2.x || 3.3.x", "*".

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type semver struct {
	maj, min, pat int
	pre           string // "" for a release
}

var reSemver = regexp.MustCompile(`^v?(\d+)(?:\.(\d+|[xX*]))?(?:\.(\d+|[xX*]))?(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// parseSemver reads one complete version ("3.10.5", "1.9.20-beta.1").
func parseSemver(s string) (semver, bool) {
	v, wild, ok := parseVersionish(s)
	if !ok || wild != 3 {
		return semver{}, false
	}
	return v, true
}

// parseVersionish reads a version that may end in x: wild says how many parts are given (1 = major only,
// 2 = major.minor, 3 = all), 0 when the whole thing is "*".
func parseVersionish(s string) (v semver, wild int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || s == "x" || s == "X" {
		return semver{}, 0, true
	}
	m := reSemver.FindStringSubmatch(s)
	if m == nil {
		return semver{}, 0, false
	}
	num := func(p string) (int, bool) {
		if p == "" || p == "x" || p == "X" || p == "*" {
			return 0, false
		}
		n, _ := strconv.Atoi(p)
		return n, true
	}
	v.maj, _ = num(m[1])
	wild = 1
	if n, given := num(m[2]); given {
		v.min, wild = n, 2
		if n, given := num(m[3]); given {
			v.pat, wild = n, 3
		}
	}
	v.pre = m[4]
	return v, wild, true
}

func (a semver) String() string {
	s := strconv.Itoa(a.maj) + "." + strconv.Itoa(a.min) + "." + strconv.Itoa(a.pat)
	if a.pre != "" {
		s += "-" + a.pre
	}
	return s
}

// cmp: -1, 0, 1. A prerelease sorts before the release it leads to.
func (a semver) cmp(b semver) int {
	for _, d := range []int{a.maj - b.maj, a.min - b.min, a.pat - b.pat} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	as, bs := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aerr == nil:
			return -1 // numbers sort before words
		case berr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	if len(as) != len(bs) {
		if len(as) < len(bs) {
			return -1
		}
		return 1
	}
	return 0
}

// A range is alternatives (||) of comparator sets (all must hold).
type verRange [][]verCmp

type verCmp struct {
	op string // ">=", ">", "<=", "<", "="
	v  semver
}

func (c verCmp) holds(v semver) bool {
	d := v.cmp(c.v)
	switch c.op {
	case ">=":
		return d >= 0
	case ">":
		return d > 0
	case "<=":
		return d <= 0
	case "<":
		return d < 0
	}
	return d == 0
}

var reHyphen = regexp.MustCompile(`^\s*(\S+)\s+-\s+(\S+)\s*$`)

func parseRange(s string) (verRange, error) {
	var out verRange
	for _, alt := range strings.Split(s, "||") {
		set, err := parseCmpSet(alt)
		if err != nil {
			return nil, err
		}
		out = append(out, set)
	}
	return out, nil
}

func parseCmpSet(s string) ([]verCmp, error) {
	s = strings.TrimSpace(s)
	if m := reHyphen.FindStringSubmatch(s); m != nil { // "3.2 - 3.10": from the low one up to the end of the high one
		lo, _, ok1 := parseVersionish(m[1])
		hi, wild, ok2 := parseVersionish(m[2])
		if !ok1 || !ok2 {
			return nil, errors.New("看不懂版本范围：" + s)
		}
		set := []verCmp{{">=", lo}}
		switch wild {
		case 1:
			set = append(set, verCmp{"<", semver{maj: hi.maj + 1}})
		case 2:
			set = append(set, verCmp{"<", semver{maj: hi.maj, min: hi.min + 1}})
		default:
			set = append(set, verCmp{"<=", hi})
		}
		return set, nil
	}
	// operators that were written with a space before the version ("< 3.11.X") join their version
	toks := strings.Fields(s)
	var joined []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t == ">=" || t == ">" || t == "<=" || t == "<" || t == "=" || t == "^" || t == "~") && i+1 < len(toks) {
			t += toks[i+1]
			i++
		}
		joined = append(joined, t)
	}
	if len(joined) == 0 {
		return nil, nil // "*": anything
	}
	var set []verCmp
	for _, t := range joined {
		op := ""
		for _, p := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
			if strings.HasPrefix(t, p) {
				op, t = p, t[len(p):]
				break
			}
		}
		v, wild, ok := parseVersionish(t)
		if !ok {
			return nil, errors.New("看不懂版本范围：" + s)
		}
		if wild == 0 { // "*"
			continue
		}
		next := func() semver { // the first version past the given part
			switch wild {
			case 1:
				return semver{maj: v.maj + 1}
			case 2:
				return semver{maj: v.maj, min: v.min + 1}
			}
			return semver{maj: v.maj, min: v.min, pat: v.pat + 1}
		}
		switch op {
		case "^":
			hi := semver{maj: v.maj + 1}
			if v.maj == 0 && wild >= 2 {
				hi = semver{maj: 0, min: v.min + 1}
				if v.min == 0 && wild == 3 {
					hi = semver{maj: 0, min: 0, pat: v.pat + 1}
				}
			}
			set = append(set, verCmp{">=", v}, verCmp{"<", hi})
		case "~":
			hi := semver{maj: v.maj, min: v.min + 1}
			if wild == 1 {
				hi = semver{maj: v.maj + 1}
			}
			set = append(set, verCmp{">=", v}, verCmp{"<", hi})
		case "", "=":
			if wild < 3 { // "3.1.x", "3.2": everything with that start
				set = append(set, verCmp{">=", v}, verCmp{"<", next()})
			} else {
				set = append(set, verCmp{"=", v})
			}
		case "<":
			if wild < 3 { // "<3.11.x" is "<3.11.0"
				set = append(set, verCmp{"<", v})
			} else {
				set = append(set, verCmp{"<", v})
			}
		case "<=":
			if wild < 3 { // "<=3.11.x" is everything below 3.12
				set = append(set, verCmp{"<", next()})
			} else {
				set = append(set, verCmp{"<=", v})
			}
		case ">":
			if wild < 3 { // ">3.11.x" is ">=3.12.0"
				set = append(set, verCmp{">=", next()})
			} else {
				set = append(set, verCmp{">", v})
			}
		case ">=":
			set = append(set, verCmp{">=", v})
		}
	}
	return set, nil
}

func (r verRange) match(v semver) bool {
	for _, set := range r {
		ok := true
		for _, c := range set {
			if !c.holds(v) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return len(r) == 0
}
