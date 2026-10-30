package planner

import (
	"net/url"
	"testing"
)

func TestHistoryFilterDateDomain(t *testing.T) {
	for _, value := range []string{"0000-01-01", "2024-02-30", "2024-2-29", "2024-02-9"} {
		if _, _, err := parseHistoryFilters(url.Values{"date_from": []string{value}}, true); err == nil {
			t.Fatal("noncanonical or PostgreSQL-invalid history date accepted", value)
		}
	}
	for _, value := range []string{"0001-01-01", "1999-12-31", "2024-02-29", "9999-12-31"} {
		if _, _, err := parseHistoryFilters(url.Values{"date_from": []string{value}}, true); err != nil {
			t.Fatal("valid history outside project planning years rejected", value, err)
		}
	}
}
