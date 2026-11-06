@id("allow:Mutation.editBio")
@subject("arg:kaid")
permit (principal, action == Users::Action::"Mutation.editBio", resource);

@id("rule.user.authz-bio")
@source("resolvers/u.go:1-3")
@subject("arg:kaid")
forbid (principal, action == Users::Action::"Mutation.editBio", resource)
when { !(((principal is Users::User && principal.attributes.contains("internal_user")) && !(resource.attributes.contains("deleted")) && context.attributes.contains("bio_flag") && (principal is Users::User && principal.localeGrants.contains({capability: "can_view_user_identity", locale: context.locale})) && principal in Users::CapExplicit::"can_view_user_identity" && resource.subjectRelations.contains("managing_coach_of"))) };

@id("allow:Mutation.login")
permit (principal, action == Users::Action::"Mutation.login", resource);

@id("rule.auth.authz-login")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Mutation.login", resource)
when { (!((principal is Users::User && principal.loggedIn && !principal.phantom)) && !(principal is Users::Service)) };

@id("allow:Mutation.notify")
permit (principal, action == Users::Action::"Mutation.notify", resource);

@id("rule.hook.authz-notify")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Mutation.notify", resource)
when { !(principal is Users::Push) };

@id("allow:Query.cdi")
permit (principal, action in [Users::Action::"Query.cdi", Users::Action::"Query.cdiAlias"], resource);

@id("rule.class.authz-cdi")
@source("resolvers/u.go:1-3")
forbid (principal, action in [Users::Action::"Query.cdi", Users::Action::"Query.cdiAlias"], resource)
when { !(resource.relations.contains("teacher_of")) };

@id("allow:Query.things")
permit (principal, action == Users::Action::"Query.things", resource);

@id("allow:Query.time")
permit (principal, action == Users::Action::"Query.time", resource);

@id("allow:Query.udi")
permit (principal, action == Users::Action::"Query.udi", resource);

@id("rule.district.authz-udi")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Query.udi", resource)
when { !((principal is Users::Service || principal in Users::CapUserAll::"can_view_user_identity" || principal in Users::CapNwea::"can_view_user_identity")) };

@id("allow:User.emails")
@subject("parent:Kaid")
permit (principal, action == Users::Action::"User.emails", resource);

@id("rule.user.authz-emails")
@source("resolvers/u.go:1-3")
@subject("parent:Kaid")
forbid (principal, action == Users::Action::"User.emails", resource)
when { (!((principal is Users::User && resource.subject == principal)) && !((principal in Users::CapUserAll::"can_view_user_identity" || (principal in Users::CapNwea::"can_view_user_identity" && resource.kmapDistrictOwned)))) };

@id("allow:view:Thing")
permit (principal, action == Users::Action::"view:Thing", resource);

@id("rule.district.authz-things")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"view:Thing", resource)
when { !((principal is Users::Service || (principal is Users::User && resource.subject == principal) || (principal in Users::CapNwea::"can_view_user_identity" && resource.relations.contains("in_kmap_district")))) };

