package genqlient

const Users_AcceptInvitation_Operation = `
mutation Users_AcceptInvitation ($inviteId: String!) {
	acceptInvitation(inviteId: $inviteId)
}
`

const Users_Mixed_Operation = `
query Users_Mixed ($kaid: String!) {
	guideStatus(kaid: $kaid)
	__typename
}
`

const Users_Own_Operation = `
mutation Users_Own ($kaid: String!) {
	addAuthEmail(kaid: $kaid)
}
`

const Users_ActorCanEditAnyContent_Operation = `
query Users_ActorCanEditAnyContent {
	user {
		canEditContent: hasPermission(name: "CAN_EDIT_CONTENT", scope: ANY_ON_CURRENT_LOCALE)
	}
}
`

const Users_UserKAIDIfActorHasCapability_Operation = `
query Users_UserKAIDIfActorHasCapability ($capabilityName: String!) {
	user {
		kaid
		hasPermission(name: $capabilityName, scope: ANY_ON_CURRENT_LOCALE)
	}
}
`

const Users_Unscoped_Operation = `
query Users_Unscoped ($name: String!) {
	user {
		a: hasPermission(name: $name)
		b: hasPermission(name: "CAN_EDIT_CONTENT")
	}
}
`

const Users_ActorCanMapStandards_Operation = `
query Users_ActorCanMapStandards ($capability: String!) {
	user {
		canMapStandards: hasPermission(name: $capability, scope: ANY_ON_CURRENT_LOCALE)
	}
}
`
