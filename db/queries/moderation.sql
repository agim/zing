-- name: ModerationSuspended :one
SELECT EXISTS(SELECT 1 FROM moderation_account WHERE owner_id=$1 AND suspended);

-- Maps an already visible reported native identity to its local owner; no credentials returned.
-- lidza:ignore L018
-- name: ModerationTargetOwner :one
SELECT owner_id FROM hosted_identity WHERE network=$1 AND server=$2 AND subject=$3 AND state='active';

-- name: ModerationCreateReport :one
INSERT INTO moderation_report(reporter_id,network,target,target_owner_id,reason,evidence_sealed,kind) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text;

-- name: ModerationReports :many
SELECT r.*,coalesce(u.name,'Deleted account')::text AS reporter_name FROM moderation_report r LEFT JOIN auth_user u ON u.subject=r.reporter_id ORDER BY r.created_at DESC,r.id DESC LIMIT 100;

-- name: ModerationReport :one
SELECT * FROM moderation_report WHERE id::text=$1;

-- name: ModerationUsers :many
SELECT u.subject,coalesce(u.name,'')::text AS name,coalesce(u.email,'')::text AS email,coalesce(m.suspended,false) AS suspended,coalesce(m.reason,'')::text AS reason FROM auth_user u LEFT JOIN moderation_account m ON m.owner_id=u.subject ORDER BY u.created_at,u.subject LIMIT 200;

-- name: ModerationActions :many
SELECT * FROM moderation_action ORDER BY created_at DESC,id DESC LIMIT 100;

-- name: ModerationAction :one
SELECT * FROM moderation_action WHERE id::text=$1;

-- name: ModerationCreateAction :one
INSERT INTO moderation_action(moderator_id,kind,network,target,reason,request_key) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(request_key) DO UPDATE SET request_key=excluded.request_key RETURNING id::text;

-- name: ModerationSetActionState :exec
UPDATE moderation_action SET state=$2,error=$3 WHERE id::text=$1;

-- name: ModerationSetSuspended :exec
INSERT INTO moderation_account(owner_id,suspended,reason) VALUES($1,$2,$3) ON CONFLICT(owner_id) DO UPDATE SET suspended=excluded.suspended,reason=excluded.reason,updated_at=now();

-- name: ModerationResolveReport :execrows
UPDATE moderation_report SET state='resolved' WHERE id::text=$1 AND state='open';

-- name: ModerationAddDestination :exec
INSERT INTO moderation_destination(action_id,network) VALUES($1,$2) ON CONFLICT(action_id,network) DO NOTHING;

-- name: ModerationDestinations :many
SELECT * FROM moderation_destination WHERE action_id::text=$1 ORDER BY network;

-- name: ModerationDestinationState :exec
UPDATE moderation_destination SET state=$3,error=$4 WHERE action_id::text=$1 AND network=$2;

-- name: ModerationHostedAccounts :many
SELECT h.* FROM hosted_identity h WHERE h.owner_id=$1 AND h.state='active' ORDER BY network;

-- Native moderation worker only, behind verified admin authorization and reported local post identity checks.
-- lidza:ignore L018
-- name: ModerationHostedSession :one
SELECT c.* FROM social_connection c WHERE c.hosted AND c.network=$1 AND c.subject=$2 AND c.server=$3;

-- name: ModerationBackendCredential :one
SELECT * FROM backend_credential WHERE server=$1;

-- name: ModerationCreateBackendCredential :one
INSERT INTO backend_credential(id,server,password_sealed) VALUES($1,$2,$3) ON CONFLICT(server) DO UPDATE SET server=excluded.server RETURNING *;

-- name: ModerationStoreBackendAccess :exec
UPDATE backend_credential SET access_sealed=$2 WHERE id=$1;

-- name: ModerationDeleteReporterData :exec
DELETE FROM moderation_report WHERE reporter_id=$1;

-- name: ModerationAnonymizeDeletedTarget :exec
UPDATE moderation_report SET target_owner_id='',evidence_sealed='' WHERE target_owner_id=$1;

-- name: ModerationDeleteOwnerState :exec
DELETE FROM moderation_account WHERE owner_id=$1;

-- name: ModerationActionByRequest :one
SELECT * FROM moderation_action WHERE request_key=$1;

-- name: ModerationOwnerExists :one
SELECT EXISTS(SELECT 1 FROM auth_user WHERE subject=$1);

-- name: ModerationOpenAppeal :one
SELECT id::text FROM moderation_report WHERE kind='appeal' AND reporter_id=$1 AND state='open' LIMIT 1;

-- name: ModerationCancelOwnerActions :exec
UPDATE moderation_action SET state='cancelled',error='Account was deleted' WHERE target=$1 AND network='all' AND state IN ('pending','failed');

-- name: ModerationNewerAction :one
SELECT EXISTS(SELECT 1 FROM moderation_action newer JOIN moderation_action old ON old.id::text=$1 WHERE newer.target=old.target AND newer.network=old.network AND newer.kind IN ('suspend','restore') AND (newer.created_at,newer.id)>(old.created_at,old.id));

-- name: ModerationRefreshConnection :exec
UPDATE social_connection SET reconnect=false WHERE owner_id=$1 AND hosted;

-- name: ModerationPostOwner :one
SELECT target_owner_id FROM moderation_report WHERE kind='post' AND network=$1 AND target=$2 AND target_owner_id<>'' ORDER BY created_at DESC LIMIT 1;
