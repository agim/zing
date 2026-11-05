-- name: AtIdentity :one
SELECT h.owner_id,h.subject,h.handle,h.server FROM hosted_identity h WHERE h.network='bluesky' AND h.state='active' AND (h.subject=$1 OR h.handle=$1) AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended);

-- name: AtPeople :many
SELECT h.subject,h.handle,h.server,p.value AS profile_value,coalesce(p.value->>'displayName',h.handle)::text AS name,coalesce(p.value->>'description','')::text AS bio,
 (SELECT count(DISTINCT f.did) FROM at_record f WHERE f.collection='app.bsky.graph.follow' AND f.subject=h.subject) AS followers,
 (SELECT count(DISTINCT f.subject) FROM at_record f WHERE f.collection='app.bsky.graph.follow' AND f.did=h.subject) AS following
FROM hosted_identity h LEFT JOIN at_record p ON p.did=h.subject AND p.collection='app.bsky.actor.profile'
WHERE h.network='bluesky' AND h.state='active' AND (h.handle ILIKE '%'||$1::text||'%' OR coalesce(p.value->>'displayName','') ILIKE '%'||$1::text||'%')
AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended)
AND NOT EXISTS(SELECT 1 FROM at_record b WHERE b.collection='app.bsky.graph.block' AND ((b.did=$2 AND b.subject=h.subject) OR (b.subject=$2 AND b.did=h.subject)))
ORDER BY h.handle,h.subject LIMIT 50;

-- name: AtSyncLock :exec
SELECT pg_advisory_xact_lock(hashtextextended($1,0));

-- name: AtSyncState :one
SELECT id,did,collection,cursor,scan_id,completed_at,error,started_at FROM at_sync_state WHERE id=$1;

-- name: AtStoreSyncState :exec
INSERT INTO at_sync_state(id,did,collection,cursor,scan_id,completed_at,error,started_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(id) DO UPDATE SET cursor=excluded.cursor,scan_id=excluded.scan_id,completed_at=excluded.completed_at,error=excluded.error,started_at=excluded.started_at;

-- name: AtPriorRecord :one
SELECT cid FROM at_record WHERE uri=$1;

-- name: AtUpsertRecord :exec
INSERT INTO at_record(uri,did,collection,cid,value,text,subject,created_at,scan_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(uri) DO UPDATE SET cid=excluded.cid,value=excluded.value,text=excluded.text,subject=excluded.subject,created_at=excluded.created_at,scan_id=excluded.scan_id,indexed_at=now();

-- name: AtPruneNotifications :exec
DELETE FROM at_notification WHERE record_uri IN (SELECT uri FROM at_record WHERE did=$1 AND collection=$2 AND scan_id<>$3 AND indexed_at<$4);

-- name: AtPruneRecords :exec
DELETE FROM at_record WHERE did=$1 AND collection=$2 AND scan_id<>$3 AND indexed_at<$4;

-- name: AtRecordByURI :one
SELECT * FROM at_record WHERE uri=$1;

-- name: AtNotify :exec
INSERT INTO at_notification(id,recipient,actor,kind,record_uri,post_uri,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING;

-- name: AtPostOwner :one
SELECT did FROM at_record WHERE uri=$1 AND collection='app.bsky.feed.post';

-- name: AtFeed :many
SELECT r.*,
 (SELECT count(DISTINCT a.did) FROM at_record a WHERE a.collection='app.bsky.feed.like' AND a.subject=r.uri) AS likes,
 (SELECT count(DISTINCT a.did) FROM at_record a WHERE a.collection='app.bsky.feed.repost' AND a.subject=r.uri) AS reposts,
 (SELECT count(*) FROM at_record a WHERE a.collection='app.bsky.feed.post' AND a.subject=r.uri) AS replies,
 EXISTS(SELECT 1 FROM at_record a WHERE a.did=sqlc.arg(viewer_did)::text AND a.collection='app.bsky.feed.like' AND a.subject=r.uri) AS liked,
 EXISTS(SELECT 1 FROM at_record a WHERE a.did=sqlc.arg(viewer_did)::text AND a.collection='app.bsky.feed.repost' AND a.subject=r.uri) AS reposted,
 h.handle,p.value AS profile_value,coalesce(p.value->>'displayName',h.handle)::text AS author_name,coalesce(p.value->>'description','')::text AS author_bio
FROM at_record r JOIN hosted_identity h ON h.subject=r.did AND h.network='bluesky' AND h.state='active'
LEFT JOIN at_record p ON p.did=r.did AND p.collection='app.bsky.actor.profile'
WHERE r.collection='app.bsky.feed.post'
AND (sqlc.arg(scope)::text='discover' OR sqlc.arg(query)::text<>'' OR r.did=sqlc.arg(viewer_did)::text OR EXISTS(SELECT 1 FROM at_record f WHERE f.did=sqlc.arg(viewer_did)::text AND f.collection='app.bsky.graph.follow' AND f.subject=r.did) OR EXISTS(SELECT 1 FROM at_record rep WHERE rep.collection='app.bsky.feed.repost' AND rep.subject=r.uri AND (rep.did=sqlc.arg(viewer_did)::text OR EXISTS(SELECT 1 FROM at_record f WHERE f.did=sqlc.arg(viewer_did)::text AND f.collection='app.bsky.graph.follow' AND f.subject=rep.did))))
AND (sqlc.arg(query)::text='' OR r.text ILIKE '%'||sqlc.arg(query)::text||'%')
AND (sqlc.arg(post_uri)::text='' OR r.uri=sqlc.arg(post_uri))
AND (sqlc.narg(cursor_at)::timestamptz IS NULL OR (r.created_at,r.uri)<(sqlc.narg(cursor_at)::timestamptz,sqlc.arg(cursor_uri)::text))
AND NOT EXISTS(SELECT 1 FROM at_record b WHERE b.collection='app.bsky.graph.block' AND ((b.did=sqlc.arg(viewer_did)::text AND b.subject=r.did) OR (b.did=r.did AND b.subject=sqlc.arg(viewer_did)::text)))
AND NOT EXISTS(SELECT 1 FROM at_preference m WHERE m.owner_id=sqlc.arg(owner_id)::text AND m.did=sqlc.arg(viewer_did)::text AND m.target=r.did AND m.kind='mute')
AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended)
AND NOT EXISTS(SELECT 1 FROM moderation_action a WHERE a.kind='remove_post' AND a.network='bluesky' AND a.target=r.uri AND a.state IN ('pending','applied','failed'))
ORDER BY r.created_at DESC,r.uri DESC LIMIT 21;

-- name: AtNotifications :many
SELECT n.id,n.kind,n.created_at,h.handle,coalesce(r.text,'')::text AS text
FROM at_notification n JOIN hosted_identity h ON h.subject=n.actor AND h.network='bluesky' AND h.state='active'
LEFT JOIN at_record r ON r.uri=n.post_uri
WHERE n.recipient=$1
AND NOT EXISTS(SELECT 1 FROM moderation_action a WHERE a.kind='remove_post' AND a.network='bluesky' AND a.target=n.post_uri AND a.state IN ('pending','applied','failed'))
AND NOT EXISTS(SELECT 1 FROM at_record b WHERE b.collection='app.bsky.graph.block' AND ((b.did=$1 AND b.subject=n.actor) OR (b.did=n.actor AND b.subject=$1)))
AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended)
AND NOT EXISTS(SELECT 1 FROM at_preference m WHERE m.owner_id=$2 AND m.did=$1 AND m.target=n.actor AND m.kind='mute')
ORDER BY n.created_at DESC,n.id DESC LIMIT 50;

-- name: AtMute :exec
INSERT INTO at_preference(owner_id,did,target,kind) VALUES($1,$2,$3,'mute') ON CONFLICT(owner_id,did,target,kind) DO NOTHING;

-- name: AtUnmute :exec
DELETE FROM at_preference WHERE owner_id=$1 AND did=$2 AND target=$3 AND kind='mute';

-- name: AtOwnActionExists :one
SELECT EXISTS(SELECT 1 FROM at_record WHERE did=$1 AND collection=$2 AND subject=$3);

-- name: AtRemoveActorRecords :exec
DELETE FROM at_record WHERE did IN (SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky');

-- name: AtRemoveOwnerPreferences :exec
DELETE FROM at_preference WHERE owner_id=$1;

-- name: AtDeleteRecordNotifications :exec
DELETE FROM at_notification WHERE record_uri=$1;

-- name: AtDeleteRecord :exec
DELETE FROM at_record WHERE uri=$1 AND did=$2;

-- name: AtDeleteActionNotifications :exec
DELETE FROM at_notification WHERE record_uri IN (SELECT uri FROM at_record WHERE did=$1 AND collection=$2 AND subject=$3);

-- name: AtDeleteActionRecords :exec
DELETE FROM at_record WHERE did=$1 AND collection=$2 AND subject=$3;

-- name: AtIdentityForOwner :one
SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky' AND state='active';

-- name: AtProfile :one
SELECT h.subject,h.handle,h.server,p.value AS profile_value,coalesce(p.value->>'displayName',h.handle)::text AS name,coalesce(p.value->>'description','')::text AS bio,
 (SELECT count(DISTINCT f.did) FROM at_record f WHERE f.collection='app.bsky.graph.follow' AND f.subject=h.subject) AS followers,
 (SELECT count(DISTINCT f.subject) FROM at_record f WHERE f.collection='app.bsky.graph.follow' AND f.did=h.subject) AS following
FROM hosted_identity h LEFT JOIN at_record p ON p.did=h.subject AND p.collection='app.bsky.actor.profile'
WHERE h.network='bluesky' AND h.state='active' AND h.subject=$1
AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended)
AND NOT EXISTS(SELECT 1 FROM at_record b WHERE b.collection='app.bsky.graph.block' AND ((b.did=$2 AND b.subject=h.subject) OR (b.subject=$2 AND b.did=h.subject)))
LIMIT 1;


-- name: AtSessionLock :exec
SELECT pg_advisory_lock(hashtextextended($1,0));

-- name: AtSessionUnlock :one
SELECT pg_advisory_unlock(hashtextextended($1,0));

-- name: AtActiveOwners :many
SELECT DISTINCT h.owner_id FROM hosted_identity h WHERE h.network='bluesky' AND h.state='active' AND NOT EXISTS(SELECT 1 FROM moderation_account m WHERE m.owner_id=h.owner_id AND m.suspended);

-- Public actor existence, checked while holding the actor index lock; never reads credentials.
-- lidza:ignore L018
-- name: AtIdentityExists :one
SELECT EXISTS(SELECT 1 FROM hosted_identity h JOIN auth_user u ON u.subject=h.owner_id WHERE h.subject=$1 AND h.network='bluesky' AND h.state='active');

-- name: AtOwnerIdentities :many
SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky' ORDER BY subject;

-- name: AtRemoveOwnerNotifications :exec
DELETE FROM at_notification WHERE actor IN (SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky') OR recipient IN (SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky');

-- name: AtRemoveOwnerScans :exec
DELETE FROM at_sync_state WHERE did IN (SELECT h.subject FROM hosted_identity h WHERE h.owner_id=$1 AND h.network='bluesky');
