@id("allow:Mutation.editBio")
@subject("arg:kaid")
permit (principal, action == Users::Action::"Mutation.editBio", resource)
when { principal is Users::User };

@id("rule.user.authz-bio")
@source("resolvers/u.go:1-3")
@subject("arg:kaid")
forbid (principal, action == Users::Action::"Mutation.editBio", resource)
when { !(((principal is Users::User && principal.attributes.contains("internal_user")) && !(resource.attributes.contains("deleted")) && context.attributes.contains("bio_flag") && (principal is Users::User && (principal.localeGrants.contains({capability: "can_view_user_identity", locale: context.locale}) || principal.localeGrants.contains({capability: "can_view_user_identity", locale: "*"}))) && principal in Users::CapExplicit::"can_view_user_identity" && resource.subjectRelations.contains("managing_coach_of"))) };

@id("allow:Mutation.login")
permit (principal, action == Users::Action::"Mutation.login", resource)
when { principal is Users::Service || principal is Users::User };

@id("rule.auth.authz-login")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Mutation.login", resource)
when { (!((principal is Users::User && principal.loggedIn && !principal.phantom)) && !(principal is Users::Service)) };

@id("allow:Mutation.notify")
@obligation("rate_limit ratelimit.ByIP key=Notify count=10 window=60 * time.Second")
permit (principal, action == Users::Action::"Mutation.notify", resource)
when { principal is Users::Push };

@id("rule.hook.authz-notify")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Mutation.notify", resource)
when { !(principal is Users::Push) };

@id("allow:Mutation.transfer")
@subject("arg:from")
@subject2("arg:to")
permit (principal, action == Users::Action::"Mutation.transfer", resource)
when { principal is Users::User };

@id("rule.user.authz-transfer-from")
@source("resolvers/u.go:1-3")
@subject("arg:from")
@subject2("arg:to")
forbid (principal, action == Users::Action::"Mutation.transfer", resource)
when { !((principal is Users::User && resource.subject == principal)) };

@id("rule.user.authz-transfer-to")
@source("resolvers/u.go:1-3")
@subject("arg:from")
@subject2("arg:to")
forbid (principal, action == Users::Action::"Mutation.transfer", resource)
when { (!((principal is Users::User && resource.subject2 == principal)) && !((principal is Users::User && resource.coaches2.contains(principal)))) };

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
permit (principal, action == Users::Action::"Query.udi", resource)
when { principal is Users::Service || principal is Users::User };

@id("rule.district.authz-udi")
@source("resolvers/u.go:1-3")
forbid (principal, action == Users::Action::"Query.udi", resource)
when { !((principal is Users::Service || (principal is Users::User && (principal.contentGrants.contains({capability: "can_view_user_identity", locale: context.locale, contentKind: "Video"}) || principal.contentGrants.contains({capability: "can_view_user_identity", locale: context.locale, contentKind: "*"}) || principal.contentGrants.contains({capability: "can_view_user_identity", locale: "*", contentKind: "Video"}) || principal.contentGrants.contains({capability: "can_view_user_identity", locale: "*", contentKind: "*"}))) || principal in Users::CapUserAll::"can_view_user_identity" || principal in Users::CapNwea::"can_view_user_identity")) };

@id("allow:User.emails")
@subject("parent:Kaid")
permit (principal, action == Users::Action::"User.emails", resource)
when { principal is Users::User };

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

