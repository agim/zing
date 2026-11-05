-- name: ChatAllowed :one
SELECT EXISTS(SELECT 1 FROM moderation_account WHERE owner_id IN ($1,$2) AND suspended) OR EXISTS(SELECT 1 FROM at_record WHERE collection='app.bsky.graph.block' AND ((did=$3 AND subject=$4) OR (did=$4 AND subject=$3))) AS blocked;

-- name: ChatMembership :one
SELECT c.state,c.initiated_by,coalesce(other.did,'') AS other_did,coalesce(other.owner_id,'') AS other_owner,coalesce(h.handle,'Deleted account') AS other_handle,(SELECT count(*) FROM chat_message WHERE conversation_id=c.id) AS message_count FROM chat_conversation c JOIN chat_member mine ON mine.conversation_id=c.id AND mine.owner_id=$1 AND mine.did=$2 LEFT JOIN chat_member other ON other.conversation_id=c.id AND other.owner_id<>$1 LEFT JOIN hosted_identity h ON h.owner_id=other.owner_id AND h.subject=other.did AND h.state='active' WHERE c.id::text=$3;

-- name: ChatRecipient :one
SELECT h.owner_id FROM hosted_identity h WHERE h.network='bluesky' AND h.subject=$1 AND h.state='active' AND EXISTS(SELECT 1 FROM auth_user u WHERE u.subject=h.owner_id);

-- name: ChatCreate :one
INSERT INTO chat_conversation(pair_key,initiated_by) VALUES($1,$2) ON CONFLICT(pair_key) DO UPDATE SET pair_key=excluded.pair_key RETURNING id::text;

-- name: ChatAddMember :exec
INSERT INTO chat_member(conversation_id,owner_id,did) VALUES($1,$2,$3) ON CONFLICT(conversation_id,owner_id) DO NOTHING;

-- name: ChatListIDs :many
SELECT c.id::text AS id,c.updated_at,m.last_read_at FROM chat_conversation c JOIN chat_member m ON m.conversation_id=c.id WHERE m.owner_id=$1 AND m.did=$2 ORDER BY c.updated_at DESC,c.id DESC LIMIT 100;

-- name: ChatLastMessage :one
SELECT text_sealed FROM chat_message WHERE conversation_id::text=$1 ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: ChatCursorExists :one
SELECT EXISTS(SELECT 1 FROM chat_message WHERE conversation_id::text=$1 AND id::text=$2);

-- Only called after owner+DID membership authorization; conversation cursor is scoped and checked.
-- lidza:ignore L018
-- name: ChatMessages :many
SELECT msg.id::text AS id,coalesce(h.handle,'Deleted account') AS sender,msg.sender_did,msg.text_sealed,msg.created_at FROM chat_message msg LEFT JOIN hosted_identity h ON h.owner_id=msg.sender_id AND h.subject=msg.sender_did AND h.state='active' WHERE msg.conversation_id::text=$1 AND ($2='' OR (msg.created_at,msg.id)<(SELECT created_at,id FROM chat_message WHERE id::text=$2 AND conversation_id::text=$1)) ORDER BY msg.created_at DESC,msg.id DESC LIMIT 51;

-- name: ChatMarkRead :exec
UPDATE chat_member SET last_read_at=now() WHERE owner_id=$1 AND did=$2 AND conversation_id::text=$3;

-- name: ChatLock :exec
SELECT pg_advisory_xact_lock(hashtextextended($1,0));

-- name: ChatPriorMessage :one
SELECT text_sealed FROM chat_message WHERE conversation_id::text=$1 AND sender_id=$2 AND client_id=$3;

-- name: ChatInsertMessage :exec
INSERT INTO chat_message(conversation_id,sender_id,sender_did,client_id,text_sealed) VALUES($1,$2,$3,$4,$5);

-- name: ChatTouch :exec
UPDATE chat_conversation SET updated_at=now() WHERE id::text=$1;

-- name: ChatSenderRead :exec
UPDATE chat_member SET last_read_at=now() WHERE conversation_id::text=$1 AND owner_id=$2;

-- name: ChatAccept :exec
UPDATE chat_conversation SET state='accepted',updated_at=now() WHERE id::text=$1;

-- Membership is filtered by m.owner_id=$1 and m.did=$2; only a selected message is returned.
-- lidza:ignore L018
-- name: ChatEvidence :one
SELECT msg.conversation_id::text AS conversation_id,msg.text_sealed,msg.sender_id,msg.sender_did FROM chat_message msg JOIN chat_member m ON m.conversation_id=msg.conversation_id AND m.owner_id=$1 AND m.did=$2 WHERE msg.id::text=$3;

-- name: ChatDeleteOwnMessages :exec
DELETE FROM chat_message WHERE sender_id=$1;

-- name: ChatCloseOwnedConversations :exec
UPDATE chat_conversation SET pair_key='deleted:'||id::text,state='closed',initiated_by='' WHERE id IN (SELECT conversation_id FROM chat_member WHERE owner_id=$1);

-- name: ChatRemoveMember :exec
DELETE FROM chat_member WHERE owner_id=$1;

-- Deletion cleanup removes only conversations with zero remaining members.
-- lidza:ignore L018
-- name: ChatDeleteEmptyMessages :exec
DELETE FROM chat_message WHERE conversation_id IN (SELECT id FROM chat_conversation WHERE NOT EXISTS(SELECT 1 FROM chat_member WHERE conversation_id=chat_conversation.id));

-- Deletion cleanup removes only conversations with zero remaining members.
-- lidza:ignore L018
-- name: ChatDeleteEmptyConversations :exec
DELETE FROM chat_conversation WHERE NOT EXISTS(SELECT 1 FROM chat_member WHERE conversation_id=chat_conversation.id);

-- Durable job publishes invalidation hints to each current member’s owner-scoped topic.
-- lidza:ignore L018
-- name: ChatNotifyMembers :many
SELECT owner_id FROM chat_member WHERE conversation_id::text=$1;

-- name: ChatOwnerConversationIDs :many
SELECT conversation_id::text FROM chat_member WHERE owner_id=$1 ORDER BY conversation_id;

-- name: ChatOwnersExist :one
SELECT count(*)=2 AS owners_exist FROM auth_user WHERE subject IN ($1,$2);
