-- Lets a trusted contact link their own SheShield account to the contact
-- record their friend/family member created for them, via a short invite
-- code. Once linked, an SOS push (the in-app alarm) can reach them by
-- their account's FCM token instead of only by SMS.
ALTER TABLE trusted_contacts ADD COLUMN linked_user_uid TEXT;
ALTER TABLE trusted_contacts ADD COLUMN invite_code TEXT;
ALTER TABLE trusted_contacts ADD COLUMN invite_expires_at TEXT;

-- Partial index: only unconsumed invites need to be looked up by code, and
-- only they need to stay unique (a consumed one is cleared to NULL).
CREATE UNIQUE INDEX idx_trusted_contacts_invite_code
	ON trusted_contacts(invite_code) WHERE invite_code IS NOT NULL;
