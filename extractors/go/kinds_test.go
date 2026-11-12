package main

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"
)

// kindModule declares kinds both ways the tests know: districts-jobs's
// models (Kind{"Name"}, a variable named after its model, RegisterKind of
// its own) and webapp's (datastore.RegisterKind(name, new(T)), a named
// constant for the name, NameKey taking the Kind itself).
// The kindModule files whose kinds the tests read.
const (
	modelsFile = "models/models.go"
	svcFile    = "svc/models.go"
)

func kindModule() map[string]string {
	return map[string]string{
		"ds/ds.go": `package ds

type Key struct{ Name string }

func NameKey(kind, name string, parent *Key) *Key { return &Key{Name: kind + name} }
`,
		"models/kind.go": `package models

type Kind struct{ Value string }

func RegisterKind(kind string, newModel any) Kind { return Kind{kind} }
`,
		modelsFile: `package models

import "example.com/app/ds"

const classroomKindValue = "ClassroomDistrictInfo"

// DistrictKind names District.
var DistrictKind = Kind{"District"}

var ClassroomDistrictInfoKind = Kind{classroomKindValue}

var EncryptionKeyKind = Kind{"UserEncryptionKey"}

var SchoolAuditLogKind = RegisterKind("SchoolAuditLog", new(SchoolAuditLog))

var OrphanKind = Kind{"Orphan"} // no Orphan type: not a kind

type BaseModel struct{ Key *ds.Key }

// District is a school district.
type District struct {
	BaseModel
	// The district's name.
	Name   string ` + "`datastore:\"name\"`" + `
	Secret string ` + "`datastore:\"-\"`" + `
	hidden int
}

type ClassroomDistrictInfo struct {
	DistrictKey *ds.Key   ` + "`datastore:\"district\"`" + `
	StudentKeys []*ds.Key ` + "`datastore:\"students\"`" + `
	AuditKey    *ds.Key
}

type EncryptionKey struct{ Secret string }

type SchoolAuditLog struct{ What string }

// MakeDistrictKey builds a District key.
func MakeDistrictKey(id string) *ds.Key { return ds.NameKey(DistrictKind.Value, id, nil) }

func Attach(c *ClassroomDistrictInfo, id string) {
	c.DistrictKey = MakeDistrictKey(id)
	c.StudentKeys = append(c.StudentKeys, MakeDistrictKey(id))
}

func NewInfo() ClassroomDistrictInfo {
	return ClassroomDistrictInfo{AuditKey: ds.NameKey(SchoolAuditLogKind.Value, "a", nil)}
}
`,
		"datastore/datastore.go": `package datastore

type Kind struct{ value string }

type Key struct{ kind string }

func RegisterKind(kind string, newModel any) Kind { return Kind{kind} }

func NameKey(k Kind, name string, parent *Key) *Key { return &Key{kind: k.value} }
`,
		svcFile: `package svc

import "example.com/app/datastore"

const widgetKindName = "Widget"

var WidgetKind = datastore.RegisterKind(widgetKindName, new(Widget))

var GadgetKind = datastore.RegisterKind("Gadget", new(Gadget))

type Widget struct {
	Key  *datastore.Key ` + "`datastore:\"-\"`" + `
	Size int
}

type Gadget struct{ WidgetKey *datastore.Key }

func Link(g *Gadget) { g.WidgetKey = datastore.NameKey(WidgetKind, "w", nil) }

func Identify(w *Widget) { w.Key = datastore.NameKey(WidgetKind, "w", nil) } // its own key

`,
	}
}

// TestKindFacts: a kind declared either way, its name a literal or a
// constant, becomes a table fact citing its model: stored columns (tag
// names; skipped, unexported and embedded fields left out), doc comments,
// stored_by when the model is named otherwise, and the references its key
// fields hold, built in place or by a function; a key slice, a key of the
// model's own kind (its identity) and a variable with no model are left
// out.
func TestKindFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/app", kindModule())
	got := map[string]map[string]string{}
	for _, f := range KindFacts(m.pkgs, m.fset, dir) {
		got[f.ID] = f.Attrs
		got[f.ID]["@"] = f.Source.Path
	}
	want := map[string]map[string]string{
		"table.kind.District": {
			attrName: "District", attrColumns: "name:string",
			attrDescription: "District is a school district.", "@": modelsFile,
			attrColumnDescriptions: `{"name":"The district's name."}`,
		},
		"table.kind.ClassroomDistrictInfo": {
			attrName:       "ClassroomDistrictInfo",
			attrColumns:    "district:*ds.Key, students:[]*ds.Key, AuditKey:*ds.Key",
			attrReferences: "AuditKey:SchoolAuditLog:null, district:District:null",
			"@":            modelsFile,
		},
		"table.kind.UserEncryptionKey": {
			attrName: "UserEncryptionKey", attrColumns: "Secret:string",
			attrStoredBy: "EncryptionKey", "@": modelsFile,
		},
		"table.kind.SchoolAuditLog": {
			attrName: "SchoolAuditLog", attrColumns: "What:string", "@": modelsFile,
		},
		"table.kind.Widget": {attrName: "Widget", attrColumns: "Size:int", "@": svcFile},
		"table.kind.Gadget": {
			attrName: "Gadget", attrColumns: "WidgetKey:*datastore.Key",
			attrReferences: "WidgetKey:Widget:null", "@": svcFile,
		},
	}
	if !maps.EqualFunc(got, want, maps.Equal) {
		for _, id := range slices.Sorted(maps.Keys(want)) {
			if !maps.Equal(got[id], want[id]) {
				t.Errorf("%s =\n%v\nwant\n%v", id, got[id], want[id])
			}
		}
		for id := range got {
			if _, ok := want[id]; !ok {
				t.Errorf("unexpected %s: %v", id, got[id])
			}
		}
	}
}

// TestSharedKindFacts: a shared kind the module reaches, by its model or
// its variable, is a table fact cited under the shared root, with the
// references the module's code gives its key fields; a shared kind
// nothing reaches is not.
func TestSharedKindFacts(t *testing.T) {
	ws := writeTree(t, map[string]string{
		"shared/models/models.go": `package models

type Kind struct{ Value string }

type Key struct{ Name string }

func NameKey(kind, name string, parent *Key) *Key { return &Key{Name: name} }

var AccountKind = Kind{"Account"}

var SessionKind = Kind{"Session"}

var LedgerKind = Kind{"Ledger"} // nothing reaches it

type Account struct{ Name string }

type Session struct{ AccountKey *Key }

type Ledger struct{ Total int }

func MakeAccountKey(id string) *Key { return NameKey(AccountKind.Value, id, nil) }
`,
		appMainFile: `package main

import "example.com/shared/models"

func main() {
	s := models.Session{}
	s.AccountKey = models.MakeAccountKey("a")
	_ = s
}
`,
	})
	shared, err := sharedRoots([]string{sharedName + "=" + filepath.Join(ws, "shared")})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ws, "app")
	m, ok := loadPackages(root, func(format string, a ...any) { t.Logf(format, a...) })
	if !ok {
		t.Fatal("workspace did not load")
	}
	var got []string
	for _, f := range SharedKindFacts(m.pkgs, m.fset, root, &shared[0]) {
		got = append(got, f.ID+" "+f.Attrs[attrReferences]+" @"+f.Source.Root+"/"+f.Source.Path)
	}
	want := []string{
		"table.kind.@pkg/Account  @pkg/models/models.go",
		"table.kind.@pkg/Session AccountKey:Account:null @pkg/models/models.go",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
