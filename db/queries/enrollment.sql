-- name: EnrollmentCreate :exec
INSERT INTO hosted_enrollment(owner_id,username) VALUES($1,$2) ON CONFLICT(owner_id) DO NOTHING;

-- name: EnrollmentOwned :one
SELECT username FROM hosted_enrollment WHERE owner_id=$1;

-- name: EnrollmentIdentity :one
SELECT username,state FROM hosted_identity WHERE owner_id=$1 AND network=$2;

-- name: EnrollmentEligible :one
SELECT EXISTS(SELECT 1 FROM auth_user u WHERE u.subject=$1 AND u.verified_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=u.subject AND m.suspended));

-- name: EnrollmentCandidates :many
SELECT e.owner_id FROM hosted_enrollment e JOIN auth_user u ON u.subject=e.owner_id
WHERE u.verified_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=e.owner_id AND m.suspended)
AND EXISTS(SELECT 1 FROM unnest($1::text[]) AS configured(network) WHERE NOT EXISTS(SELECT 1 FROM hosted_identity h WHERE h.owner_id=e.owner_id AND h.network=configured.network AND h.state='active')
AND NOT EXISTS(SELECT 1 FROM job j WHERE j.kind='zing.provision-identity' AND j.payload->>'ownerId'=e.owner_id AND j.payload->>'network'=configured.network AND j.state='failed'))
ORDER BY e.created_at,e.owner_id LIMIT 100;

-- name: EnrollmentExhausted :one
SELECT EXISTS(SELECT 1 FROM job WHERE kind='zing.provision-identity' AND payload->>'ownerId'=sqlc.arg(owner_id)::text AND payload->>'network'=sqlc.arg(network)::text AND state='failed');

-- name: EnrollmentDelete :exec
DELETE FROM hosted_enrollment WHERE owner_id=$1;
