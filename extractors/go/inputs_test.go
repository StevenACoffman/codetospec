package main

import (
	"slices"
	"testing"
)

// checkModule is a service whose access check reads model fields (one
// promoted from an embedded struct), names a capability and a flag, and
// queries the datastore by kind and property: what the policy inventory
// asks of a check.
func checkModule() map[string]string {
	return map[string]string{
		"datastore/datastore.go": `package datastore

type Query struct{}

func NewQuery(kind string) *Query { return &Query{} }

func (q *Query) FilterField(field, op string, value any) *Query { return q }
`,
		"models/udi.go": `package models

const UserDistrictInfoKind = "UserDistrictInfo"

type Base struct {
	DeletedAt *int ` + "`datastore:\"deleted_at\"`" + `
}

type UserDistrictInfo struct {
	Base
	Kaid             string   ` + "`datastore:\"kaid\"`" + `
	StudentOfCDIKeys []string ` + "`datastore:\"student_of\"`" + `
}
`,
		"capabilities/capabilities.go": `package capabilities

type CapabilityName string

var CanViewCDI CapabilityName = "CAN_VIEW_CDI"

var Registry = map[string]int{}
`,
		"flags/flags.go": `package flags

type FlagName string

const MoaEnforcement FlagName = "moa-enforcement"
`,
		"permissions/permissions.go": `package permissions

import (
	"example.com/svc/capabilities"
	"example.com/svc/datastore"
	"example.com/svc/flags"
	"example.com/svc/models"
)

func has(c capabilities.CapabilityName) bool { return c != "" }

func CanViewCDI(udi *models.UserDistrictInfo, cdi string) bool {
	_ = flags.MoaEnforcement
	_ = capabilities.Registry
	if udi.DeletedAt != nil {
		return false
	}
	_ = datastore.NewQuery(models.UserDistrictInfoKind).FilterField("kaid", "=", udi.Kaid)
	return has(capabilities.CanViewCDI) && slicesContains(udi.StudentOfCDIKeys, cdi)
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
`,
	}
}

// checkFile is the access check's file in checkModule.
const checkFile = "permissions/permissions.go"

// TestReadFacts: a function's reads name each struct field by the struct
// declaring it, a promoted field included, slices included.
func TestReadFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/svc", checkModule())
	var got []string
	for _, f := range ReadFacts(m.pkgs, m.fset, dir) {
		if f.Source.Path == checkFile && f.Source.Lines == "12-20" {
			got = append(got, f.Attrs[attrField])
		}
	}
	want := []string{
		"models.Base.DeletedAt",
		"models.UserDistrictInfo.Kaid",
		"models.UserDistrictInfo.StudentOfCDIKeys",
	}
	if !slices.Equal(got, want) {
		t.Errorf("reads of CanViewCDI = %v, want %v", got, want)
	}
}

// TestValueFacts: a function's uses of package-level constants and of
// variables of a basic type carry the value's type and literal value, so a
// capability or a flag is recognized by its type; a variable holding data
// is left out.
func TestValueFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/svc", checkModule())
	got := map[string][2]string{} // name → type, value
	for _, f := range ValueFacts(m.pkgs, m.fset, dir) {
		if f.Source.Path == checkFile && f.Source.Lines == "12-20" {
			got[f.Attrs[attrName]] = [2]string{f.Attrs[attrType], f.Attrs[attrValue]}
		}
	}
	want := map[string][2]string{
		"capabilities.CanViewCDI":     {"capabilities.CapabilityName", "CAN_VIEW_CDI"},
		"flags.MoaEnforcement":        {"flags.FlagName", "moa-enforcement"},
		"models.UserDistrictInfoKind": {"string", "UserDistrictInfo"},
	}
	for name, w := range want {
		t.Run(name, func(t *testing.T) {
			if got[name] != w {
				t.Errorf("type, value = %v, want %v", got[name], w)
			}
		})
	}
	if _, found := got["capabilities.Registry"]; found {
		t.Error("a map variable became a value fact")
	}
}

// TestLoadFacts: a function's datastore query names the kind it loads,
// by the constant's value, and the properties it filters by.
func TestLoadFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/svc", checkModule())
	var got []string
	for _, f := range LoadFacts(m.pkgs, m.fset, dir) {
		if f.Source.Path == checkFile {
			got = append(got, f.Attrs[attrDatastoreKind]+" by "+f.Attrs[attrProperties])
		}
	}
	if want := []string{"UserDistrictInfo by kaid"}; !slices.Equal(got, want) {
		t.Errorf("loads = %v, want %v", got, want)
	}
}

// TestStructTags: a struct fact records each field's datastore name, so a
// query's property maps back to its field; slices included.
func TestStructTags(t *testing.T) {
	m, dir := loadModule(t, "example.com/svc", checkModule())
	got := map[string]string{}
	for _, f := range StructFacts(m.pkgs, m.fset, dir) {
		got[f.Attrs[attrName]] = f.Attrs[attrTags]
	}
	want := map[string]string{
		"models.Base":             "DeletedAt:deleted_at",
		"models.UserDistrictInfo": "Kaid:kaid,StudentOfCDIKeys:student_of",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s tags = %q, want %q", name, got[name], w)
		}
	}
}
