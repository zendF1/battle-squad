package equipment

import (
	"encoding/json"
	"testing"
)

func TestRecipeMaterialReadsBothKeys(t *testing.T) {
	var mats []RecipeMaterial
	if err := json.Unmarshal([]byte(`[{"materialId":"a","quantity":2},{"material_id":"b","quantity":3}]`), &mats); err != nil {
		t.Fatal(err)
	}
	if len(mats) != 2 || mats[0] != (RecipeMaterial{"a", 2}) || mats[1] != (RecipeMaterial{"b", 3}) {
		t.Fatalf("got %+v", mats)
	}
	out, err := json.Marshal(mats[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"materialId":"b","quantity":3}` {
		t.Fatalf("marshal %s", out)
	}
}
