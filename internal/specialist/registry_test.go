package specialist

import "testing"

func TestRegistryExposesNativeSpecialists(t *testing.T) {
	t.Parallel()

	surveyor, err := Resolve("surveyor")
	if err != nil {
		t.Fatal(err)
	}
	if surveyor.DisplayName != "Surveyor" || !surveyor.ReportOnly {
		t.Fatalf("Resolve(surveyor) = %#v", surveyor)
	}

	scrutineer, err := Resolve("scrutineer")
	if err != nil {
		t.Fatal(err)
	}
	if scrutineer.DisplayName != "Scrutineer" || !scrutineer.ReportOnly {
		t.Fatalf("Resolve(scrutineer) = %#v", scrutineer)
	}

	if _, err := Resolve("planner"); err == nil {
		t.Fatal("Resolve(planner) accepted a non-native specialist kind")
	}

	kinds := List()
	if len(kinds) != 2 || kinds[0].Kind != "scrutineer" || kinds[1].Kind != "surveyor" {
		t.Fatalf("List() = %#v", kinds)
	}
}
