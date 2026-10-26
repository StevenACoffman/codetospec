package roles

// A role grants its capabilities: one rule each.
var Supporter = RegisterRoleDefinition(RoleDefinition{
	Name: "supporter",
	Description: "Can view and edit users and access Devadmin support " +
		"functions.",
	CapabilitySet: []capabilities.Capability{
		capabilities.CanViewUserIdentity,
		capabilities.CanChangeUserData,
	},
})

var Observer = RegisterRoleDefinition(RoleDefinition{
	Name:          "observer",
	CapabilitySet: []capabilities.Capability{},
})
