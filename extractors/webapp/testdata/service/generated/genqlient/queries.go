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
