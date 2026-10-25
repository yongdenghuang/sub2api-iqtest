package iqtest

import (
	"reflect"
	"testing"
)

func TestParseIDList(t *testing.T) {
	ids, err := parseIDList(" 3,1, ,12 ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{3, 1, 12}) {
		t.Fatalf("got %v", ids)
	}
	if ids, err := parseIDList("", 10); err != nil || len(ids) != 0 {
		t.Fatalf("empty: %v %v", ids, err)
	}
	for _, bad := range []string{"1,x", "0", "-4"} {
		if _, err := parseIDList(bad, 10); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
	if _, err := parseIDList("1,2,3", 2); err == nil {
		t.Fatal("over max should fail")
	}
}
