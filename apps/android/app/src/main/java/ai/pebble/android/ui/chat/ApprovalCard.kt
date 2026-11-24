package ai.pebble.android.ui.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.PendingApproval
import ai.pebble.android.data.errText
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject

private val approvalPrettyJson = Json { prettyPrint = true }

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ApprovalCardView(
    store: PebbleStore,
    approval: PendingApproval,
    pal: BotAccent,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var reason by remember(approval.id) { mutableStateOf("") }
    var showRaw by remember(approval.id) { mutableStateOf(false) }
    var submitting by remember(approval.id) { mutableStateOf(false) }
    var err by remember(approval.id) { mutableStateOf("") }

    val kind = approval.kind.orEmpty().ifEmpty { "other" }
    val kindKey = "permKind_$kind"
    val kindTranslated = store.t(kindKey)
    val kindLabel = if (kindTranslated == kindKey) kind else kindTranslated
    val (kindBg, kindFg) = when (kind) {
        "shell" -> p.pink to p.pinkInk
        "write" -> p.lilac to p.lilacInk
        "read" -> p.sage to p.sageInk
        else -> p.s1 to p.text2
    }

    val opts = approval.options.orEmpty()
    val allowOnceOpt = opts.firstOrNull { it.kind == "allow_once" }
    val allowAlwaysOpt = opts.firstOrNull { it.kind == "allow_always" }
    val rejectOpt = opts.firstOrNull { it.kind == "reject" }

    val rawPretty = remember(approval.id, approval.rawInput) {
        val raw = approval.rawInput ?: return@remember ""
        if (raw.isEmpty()) "" else runCatching {
            approvalPrettyJson.encodeToString(JsonObject.serializer(), raw)
        }.getOrDefault("")
    }

    fun submit(decision: String, optionId: String?, denyReason: String?) {
        if (submitting) return
        submitting = true
        err = ""
        scope.launch {
            try {
                store.replyApproval(
                    id = approval.id,
                    decision = decision,
                    optionId = optionId,
                    reason = denyReason,
                )
            } catch (e: Throwable) {
                err = errText(e, store.t("updateFailed"))
            } finally {
                submitting = false
            }
        }
    }

    Surface(
        color = p.card,
        shape = PebbleDimens.cardShape,
        modifier = Modifier
            .fillMaxWidth()
            .border(1.dp, p.busy.copy(alpha = 0.45f), PebbleDimens.cardShape),
    ) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                TagPill(text = kindLabel, bg = kindBg, fg = kindFg)
                Text(
                    text = approval.toolName,
                    color = p.text,
                    fontFamily = MonoFont,
                    fontSize = PebbleFont.cardTitle,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                TagPill(
                    text = store.t("approvalWaitingBadge"),
                    bg = p.busy.copy(alpha = 0.16f),
                    fg = p.busy,
                )
            }

            val title = approval.title.orEmpty().trim()
            if (title.isNotEmpty() && title != approval.toolName) {
                Text(
                    text = title,
                    color = p.text,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                )
            }

            val summary = approval.summary.orEmpty().trim()
            if (summary.isNotEmpty()) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .background(p.s1)
                        .border(0.5.dp, p.s2, PebbleDimens.rowShape)
                        .horizontalScroll(rememberScrollState())
                        .padding(10.dp),
                ) {
                    Text(
                        text = summary,
                        color = p.text,
                        fontFamily = MonoFont,
                        fontSize = PebbleFont.secondary,
                    )
                }
            }

            if (rawPretty.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(
                        modifier = Modifier
                            .clip(PebbleDimens.rowShape)
                            .clickable { showRaw = !showRaw }
                            .padding(vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Icon(
                            painter = painterResource(if (showRaw) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                            contentDescription = null,
                            tint = p.text2,
                            modifier = Modifier.size(16.dp),
                        )
                        Text(store.t("approvalParams"), color = p.text2, fontSize = PebbleFont.secondary)
                    }
                    AnimatedVisibility(visible = showRaw) {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .background(p.s1)
                                .border(0.5.dp, p.s2, PebbleDimens.rowShape)
                                .horizontalScroll(rememberScrollState())
                                .padding(10.dp),
                        ) {
                            Text(
                                text = rawPretty,
                                color = p.text2,
                                fontFamily = MonoFont,
                                fontSize = PebbleFont.caption,
                            )
                        }
                    }
                }
            }

            OutlinedTextField(
                value = reason,
                onValueChange = { reason = it },
                enabled = !submitting,
                placeholder = { Text(store.t("approvalReasonPh"), fontSize = PebbleFont.secondary) },
                singleLine = true,
                textStyle = TextStyle(color = p.text, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )

            if (err.isNotEmpty()) {
                Text(err, color = p.danger, fontSize = PebbleFont.secondary)
            }

            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                OutlinedButton(
                    onClick = { submit("reject", rejectOpt?.id, reason.trim().ifEmpty { null }) },
                    enabled = !submitting,
                    contentPadding = PaddingValues(horizontal = 14.dp, vertical = 8.dp),
                ) {
                    Text(store.t("approvalReject"), color = p.text, fontSize = PebbleFont.body)
                }
                if (allowAlwaysOpt != null) {
                    OutlinedButton(
                        onClick = { submit("allow_always", allowAlwaysOpt.id, null) },
                        enabled = !submitting,
                        contentPadding = PaddingValues(horizontal = 14.dp, vertical = 8.dp),
                    ) {
                        Text(store.t("approvalAllowAlways"), color = p.text, fontSize = PebbleFont.body)
                    }
                }
                Button(
                    onClick = { submit("allow_once", allowOnceOpt?.id, null) },
                    enabled = !submitting,
                    colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                    contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                ) {
                    Text(store.t("approvalAllowOnce"), fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}
