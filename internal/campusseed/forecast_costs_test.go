package campusseed

import "testing"

func TestForecastCostAmountsTellARefreshStory(t *testing.T) {
	const replacement int64 = 100_000
	dueNow := forecastCostAmounts(replacement, 0)
	if dueNow["estimated"] != replacement || dueNow["actual"] != 58_000 || dueNow["committed"] != 92_000 ||
		dueNow["normalized_real"] != 103_000 || dueNow["tco"] != 128_000 {
		t.Fatalf("unexpected due-now amounts %#v", dueNow)
	}
	nextYear := forecastCostAmounts(replacement, 1)
	if nextYear["actual"] != 22_000 || nextYear["committed"] != 80_000 {
		t.Fatalf("unexpected next-year amounts %#v", nextYear)
	}
	later := forecastCostAmounts(replacement, 4)
	if later["actual"] != 0 || later["committed"] != 0 || later["estimated"] != replacement || later["tco"] != 128_000 {
		t.Fatalf("later years should keep quotes and TCO without booked spend, got %#v", later)
	}
	if got := forecastCostAmounts(0, 0); len(got) != 0 {
		t.Fatalf("expected empty amounts for a zero replacement cost, got %#v", got)
	}
}
