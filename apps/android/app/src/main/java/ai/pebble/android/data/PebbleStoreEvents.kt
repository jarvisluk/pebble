package ai.pebble.android.data

import ai.pebble.android.i18n.L
import ai.pebble.android.notify.NotificationHelper
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement

internal fun PebbleStore.startStreamImpl(url: String, tok: String) {
    stream.start(
        scope = scope,
        base = url,
        token = tok,
        onStatus = { ok -> online = ok },
        onUnauthorized = {
            connState = ConnState.UNAUTHORIZED
            connError = L.t("needsToken")
        },
        onEvent = { type, payload -> handleEvent(type, payload, url, tok) },
    )
}

private fun PebbleStore.handleEvent(type: String, p: JsonObject, url: String, tok: String) {
    when (type) {
        "message" -> {
            val bid = p["botId"]?.asDisplayString() ?: return
            val m = runCatching { HubJson.decodeFromJsonElement<ChatMessage>(p) }.getOrNull() ?: return
            pushIncoming(bid, m)
        }
        "message_updated" -> {
            val bid = p["botId"]?.asDisplayString() ?: return
            val m = runCatching { HubJson.decodeFromJsonElement<ChatMessage>(p) }.getOrNull() ?: return
            updateIncoming(bid, m)
        }
        "bot_status" -> {
            val st = runCatching { HubJson.decodeFromJsonElement<BotStatus>(p) }.getOrNull() ?: return
            val wasBusy = bots.firstOrNull { it.id == st.id }?.isBusy == true
            upsertBot(st)
            if (st.isBusy) {
                activeBusyBots.add(st.id)
                if (!wasBusy) botSteps[st.id] = 0
                val tool = tools[st.id]
                val statusLabel = if (!tool.isNullOrBlank()) L.t("callingTool", mapOf("tool" to tool)) else L.t("thinking")
                if (!inForeground) {
                    NotificationHelper.updateBotActivity(appContext, st.id, st.displayName, statusLabel, botSteps[st.id] ?: 0)
                }
            } else {
                if (tools.containsKey(st.id)) tools.remove(st.id)
                val hadBusy = activeBusyBots.remove(st.id) || wasBusy
                val steps = botSteps.remove(st.id) ?: 0
                if (hadBusy && !inForeground) {
                    val preview = last[st.id]?.let { plainPreview(it.body) }.orEmpty()
                    val summary = if (preview.isNotEmpty()) {
                        preview.take(90)
                    } else if (steps == 1) {
                        L.t("notifStepDoneOne")
                    } else if (steps > 1) {
                        L.t("notifStepsDone", mapOf("n" to steps))
                    } else {
                        ""
                    }
                    NotificationHelper.endBotActivity(appContext, st.id, st.displayName, summary, notify = activeChat != st.id)
                } else {
                    NotificationHelper.cancelBotActivity(appContext, st.id)
                }
            }
            syncBackgroundService()
        }
        "tool_use" -> {
            val bid = p["botId"]?.asDisplayString() ?: return
            val tool = p["tool"]?.asDisplayString().orEmpty()
            if (tools[bid] != tool) tools[bid] = tool
            if (tool.isNotBlank()) {
                val steps = (botSteps[bid] ?: 0) + 1
                botSteps[bid] = steps
                if (!inForeground) {
                    val name = bots.firstOrNull { it.id == bid }?.displayName ?: bid
                    NotificationHelper.updateBotActivity(
                        appContext,
                        bid,
                        name,
                        L.t("callingTool", mapOf("tool" to tool)),
                        steps,
                    )
                }
            }
        }
        "bot_created" -> {
            val st = runCatching { HubJson.decodeFromJsonElement<BotStatus>(p) }.getOrNull() ?: return
            if (bots.none { it.id == st.id }) bots = bots + st
        }
        "bot_deleted" -> {
            val bid = p["id"]?.asDisplayString() ?: p["botId"]?.asDisplayString() ?: return
            removeBotState(bid)
        }
        "nodes_changed" -> {
            val arr = p["nodes"] as? JsonArray ?: return
            nodes = runCatching { HubJson.decodeFromJsonElement<List<NodeInfo>>(arr) }.getOrDefault(nodes)
        }
        "desktop_target" -> {
            val at = runCatching { HubJson.decodeFromJsonElement<ActiveDesktopTarget>(p) }.getOrNull() ?: return
            activeDesktopTarget = at
        }
        "stage_open" -> {
            val ev = runCatching { HubJson.decodeFromJsonElement<StageOpenEvent>(p) }.getOrNull() ?: return
            pendingStageOpen = ev
        }
        "takeover" -> {
            val arr = p["items"] as? JsonArray ?: return
            val list = runCatching { HubJson.decodeFromJsonElement<List<TakeoverRequest>>(arr) }.getOrDefault(emptyList())
            takeovers = list
            val nextIds = list.map { it.id }.toSet()
            for (oldId in activeTakeoverIds - nextIds) {
                NotificationHelper.cancelTakeoverAlert(appContext, oldId)
            }
            for (item in list) {
                if (item.id !in activeTakeoverIds && !inForeground) {
                    NotificationHelper.showTakeoverAlert(
                        appContext,
                        item.id,
                        item.reason.orEmpty(),
                        item.target ?: "local",
                    )
                }
            }
            activeTakeoverIds.clear()
            activeTakeoverIds.addAll(nextIds)
        }
        "questions" -> {
            val arr = p["items"] as? JsonArray ?: return
            questions = runCatching { HubJson.decodeFromJsonElement<List<PendingQuestion>>(arr) }.getOrDefault(emptyList())
        }
        "approvals" -> {
            val arr = p["items"] as? JsonArray ?: return
            val list = runCatching { HubJson.decodeFromJsonElement<List<PendingApproval>>(arr) }.getOrDefault(emptyList())
            approvals = list
            val nextIds = list.map { it.id }.toSet()
            for (oldId in activeApprovalIds - nextIds) {
                NotificationHelper.cancelApprovalAlert(appContext, oldId)
            }
            for (item in list) {
                if (item.id !in activeApprovalIds && (!inForeground || activeChat != item.botId)) {
                    val bName = item.botName ?: bots.firstOrNull { it.id == item.botId }?.displayName ?: item.botId
                    val label = item.toolName.ifBlank { item.title.orEmpty() }
                    val sum = item.summary.orEmpty().trim()
                    val body = listOf(label, sum).filter { it.isNotEmpty() }.joinToString(": ")
                    NotificationHelper.showApprovalAlert(
                        appContext,
                        item.id,
                        item.botId,
                        bName,
                        body,
                    )
                }
            }
            activeApprovalIds.clear()
            activeApprovalIds.addAll(nextIds)
        }
        "routine_updated", "routine_run" -> {
            val bid = p["botId"]?.asDisplayString() ?: return
            scope.launch {
                runCatching { ApiClient(url, tok).call<RoutinesResp>("/api/bots/${encodeURIComponent(bid)}/routines") }
                    .getOrNull()?.routines?.let { routines[bid] = it }
            }
        }
    }
}
