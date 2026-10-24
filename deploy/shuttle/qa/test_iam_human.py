"""Synthetic pure IAM target/body guards; no credentials, HTTP or runtime."""
import unittest
import iam_human as iam
import iam_roles as roles


class HumanGuardTest(unittest.TestCase):
    def test_subject_owner_human_and_username_are_all_required(self):
        user = {"id": "test-human", "details": {"resourceOwner": "local-org"}, "human": {}, "userName": "qa-user"}
        iam.selected_human(user, "test-human", "local-org", "qa-user", {"service-actor"})
        for patch in ({"id": "other"}, {"details": {"resourceOwner": "foreign"}}, {"machine": {}}, {"userName": "changed"}):
            with self.assertRaises(ValueError):
                iam.selected_human({**user, **patch}, "test-human", "local-org", "qa-user", {"service-actor"})
        with self.assertRaises(ValueError):
            iam.selected_human(user, "test-human", "local-org", "qa-user", {"test-human"})

    def test_recreation_cannot_inject_subject_roles_or_links(self):
        body = {"userName": "qa-user", "profile": {"firstName": "QA", "lastName": "Human"}, "email": {"email": "qa@telegram.invalid", "isEmailVerified": True}}
        self.assertEqual(iam.create_body(body, "qa-user"), body)
        for forbidden in ("id", "roles", "idpLinks"):
            with self.assertRaises(ValueError):
                iam.create_body({**body, forbidden: []}, "qa-user")

    def test_grant_path_user_is_not_authority(self):
        grant = {"id": "grant", "userId": "human", "projectId": "api", "projectGrantId": "", "details": {"resourceOwner": "org"}, "state": "USER_GRANT_STATE_ACTIVE", "roleKeys": ["other-role"]}
        roles.selected_grant(grant, "human", "api", "org")
        for patch in ({"userId": "stranger"}, {"projectId": "loom"}, {"details": {"resourceOwner": "foreign"}}, {"projectGrantId": "delegated"}, {"state": "USER_GRANT_STATE_INACTIVE"}):
            with self.assertRaises(ValueError):
                roles.selected_grant({**grant, **patch}, "human", "api", "org")

    def test_unknown_grant_search_is_not_empty(self):
        self.assertEqual(roles.grant_list({"details": {}}), [])
        self.assertEqual(roles.grant_list({"details": {"totalResult": "0"}}), [])
        for bad in ({}, {"error": "denied", "details": {}}, {"details": {"totalResult": False}}, {"details": {"totalResult": 0.0}}, {"details": {"totalResult": "1"}}):
            with self.assertRaises(ValueError):
                roles.grant_list(bad)


if __name__ == "__main__":
    unittest.main()
