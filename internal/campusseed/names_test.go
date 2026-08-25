package campusseed

import (
	"strings"
	"testing"
)

func TestCampusNameListsAreUnique(t *testing.T) {
	for _, names := range [][]string{campusGivenNames, campusFamilyNames, campusMiddleNames} {
		seen := map[string]struct{}{}
		for _, name := range names {
			if name == "" || strings.Contains(name, " ") {
				t.Fatalf("invalid name %q", name)
			}
			key := strings.ToLower(name)
			if _, exists := seen[key]; exists {
				t.Fatalf("duplicate name %q", name)
			}
			seen[key] = struct{}{}
		}
	}
	if len(campusGivenNames) < 180 || len(campusFamilyNames) < 200 {
		t.Fatalf("expected expanded name lists, got %d given and %d family names", len(campusGivenNames), len(campusFamilyNames))
	}
}

func TestCampusPersonNamesAreDeterministicAndUnique(t *testing.T) {
	seen := map[string]int{}
	firsts := map[string]int{}
	lasts := map[string]int{}
	aaronClark := 0
	for index := 0; index < campusNamePopulation; index++ {
		person := campusGeneratedName(index)
		if person.Display == "" || person.First == "" || person.Last == "" {
			t.Fatalf("empty generated name at %d: %#v", index, person)
		}
		if other, exists := seen[person.Display]; exists {
			t.Fatalf("display name %q repeats at %d and %d", person.Display, other, index)
		}
		seen[person.Display] = index
		firsts[person.First]++
		lasts[person.Last]++
		if person.First == "Aaron" && person.Last == "Clark" {
			aaronClark++
		}
	}
	if campusGeneratedName(0) != campusGeneratedName(0) || campusEmployeeName(7) != campusGeneratedName(7) {
		t.Fatal("generated names must be deterministic")
	}
	if campusStudentName(0) != campusGeneratedName(EmployeeCount) {
		t.Fatal("student names must continue the shared campus roster")
	}
	if len(firsts) < 150 || len(lasts) < 180 {
		t.Fatalf("expected a broad name mix, got %d first and %d last names", len(firsts), len(lasts))
	}
	if aaronClark > 3 {
		t.Fatalf("Aaron Clark should be rare, not a looping default, got %d", aaronClark)
	}
}

func TestCampusNameFrequenciesFollowRankCurve(t *testing.T) {
	campusNamesOnce.Do(buildCampusNames)
	firstCounts := map[string]int{}
	lastCounts := map[string]int{}
	for _, person := range campusNames {
		firstCounts[person.First]++
		lastCounts[person.Last]++
	}
	if firstCounts[campusGivenNames[0]] <= firstCounts[campusGivenNames[len(campusGivenNames)-1]] {
		t.Fatalf("most common given name %q should outrank the rarest %q: %d vs %d",
			campusGivenNames[0], campusGivenNames[len(campusGivenNames)-1],
			firstCounts[campusGivenNames[0]], firstCounts[campusGivenNames[len(campusGivenNames)-1]])
	}
	if lastCounts[campusFamilyNames[0]] <= lastCounts[campusFamilyNames[len(campusFamilyNames)-1]] {
		t.Fatalf("most common family name %q should outrank the rarest %q: %d vs %d",
			campusFamilyNames[0], campusFamilyNames[len(campusFamilyNames)-1],
			lastCounts[campusFamilyNames[0]], lastCounts[campusFamilyNames[len(campusFamilyNames)-1]])
	}
	if lastCounts["Smith"] < lastCounts["Clark"]*2 {
		t.Fatalf("Smith should appear substantially more often than Clark: smith=%d clark=%d", lastCounts["Smith"], lastCounts["Clark"])
	}
	if len(campusNames) != campusNamePopulation {
		t.Fatalf("roster size=%d want %d", len(campusNames), campusNamePopulation)
	}
}
