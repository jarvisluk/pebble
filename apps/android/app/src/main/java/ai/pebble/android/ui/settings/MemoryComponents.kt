package ai.pebble.android.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryIndexDoc
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors

fun countMemoryLines(content: String): Int {
    val trimmed = content.trimEnd('\n', '\r')
    if (trimmed.isEmpty()) return 0
    return trimmed.count { it == '\n' } + 1
}

fun parseMemoryTags(raw: String): List<String> =
    raw.split(',', '，').map { it.trim() }.filter { it.isNotEmpty() } // i18n:allow-cjk

fun localizedMemoryStatus(status: String, store: PebbleStore, candidateCount: Int = 0): String = when (status) {
    "active" -> store.t("memStatusActive")
    "candidate" -> if (candidateCount > 0) {
        store.t("memStatusCandidateCount", mapOf("n" to candidateCount))
    } else {
        store.t("memStatusCandidate")
    }
    "superseded" -> store.t("memStatusSuperseded")
    "archived" -> store.t("memStatusArchived")
    "all" -> store.t("memStatusAll")
    else -> status
}

@Composable
fun MemoryIndexCollapsible(
    store: PebbleStore,
    title: String,
    pathFallback: String,
    doc: MemoryIndexDoc?,
    content: String,
    dirty: Boolean,
    saving: Boolean,
    accent: Color,
    onAccent: Color,
    onContentChange: (String) -> Unit,
    onSave: () -> Unit,
    initiallyExpanded: Boolean = false,
) {
    val p = LocalPalette.current
    var expanded by remember { mutableStateOf(initiallyExpanded) }
    val currentLines = if (dirty) countMemoryLines(content) else (doc?.lines ?: countMemoryLines(content))
    val maxLines = doc?.maxLines ?: 0
    val overLimit = maxLines > 0 && currentLines > maxLines
    val lineLabel = if (maxLines > 0) {
        store.t("memIndexLines", mapOf("lines" to currentLines, "max" to maxLines))
    } else {
        "$currentLines"
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(PebbleDimens.rowShape)
                .clickable { expanded = !expanded }
                .padding(vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = title,
                color = p.text,
                fontSize = PebbleFont.body,
                fontWeight = FontWeight.Medium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            TagPill(
                text = lineLabel,
                bg = if (overLimit) p.pink else p.s1,
                fg = if (overLimit) p.pinkInk else p.text2,
            )
            Icon(
                painter = painterResource(if (expanded) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                contentDescription = null,
                tint = p.mute,
            )
        }

        AnimatedVisibility(visible = expanded) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = content,
                    onValueChange = onContentChange,
                    placeholder = { Text(store.t("memIndexPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                    textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                    minLines = 5,
                    maxLines = 16,
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (overLimit) {
                    Text(
                        text = store.t("memIndexOverLimit", mapOf("max" to maxLines)),
                        color = p.danger,
                        fontSize = PebbleFont.caption,
                    )
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = doc?.path?.takeIf { it.isNotBlank() } ?: pathFallback,
                        color = p.mute,
                        fontSize = PebbleFont.caption,
                        fontFamily = MonoFont,
                        maxLines = 1,
                        overflow = TextOverflow.MiddleEllipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Button(
                        onClick = onSave,
                        enabled = dirty && !saving,
                        colors = ButtonDefaults.buttonColors(containerColor = accent, contentColor = onAccent),
                    ) {
                        if (saving) {
                            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = onAccent)
                        } else {
                            Text(store.t("save"), fontSize = PebbleFont.secondary)
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun MemoryStatusFilterRow(
    store: PebbleStore,
    statuses: List<String>,
    selected: String,
    candidateCount: Int,
    accent: Color,
    onAccent: Color,
    onSelect: (String) -> Unit,
) {
    val p = LocalPalette.current
    FlowRow(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        statuses.forEach { st ->
            val active = selected == st
            val label = localizedMemoryStatus(st, store, if (st == "candidate") candidateCount else 0)
            Surface(
                color = if (active) accent else p.s1,
                shape = CircleShape,
                modifier = Modifier
                    .clip(CircleShape)
                    .clickable { onSelect(st) },
            ) {
                Text(
                    text = label,
                    color = if (active) onAccent else p.text2,
                    fontSize = PebbleFont.secondary,
                    fontWeight = if (active) FontWeight.SemiBold else FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                )
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun MemoryItemCardRow(
    store: PebbleStore,
    item: MemoryItem,
    accent: Color,
    showTags: Boolean = true,
    showScope: Boolean = false,
    busy: Boolean = false,
    onTogglePin: (() -> Unit)? = null,
    onEdit: (() -> Unit)? = null,
    onApprove: (() -> Unit)? = null,
    onDismiss: (() -> Unit)? = null,
    onArchive: (() -> Unit)? = null,
    onRestore: (() -> Unit)? = null,
    onDelete: (() -> Unit)? = null,
) {
    val p = LocalPalette.current
    val statusVal = item.status.orEmpty().lowercase()
    val bodyText = item.text?.takeIf { it.isNotBlank() } ?: item.snippet.orEmpty()

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        FlowRow(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(
                text = localizedMemoryKind(item.kind, store),
                color = p.sageInk,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
            )
            if (item.pin == true) {
                Surface(color = p.s1, shape = CircleShape) {
                    Row(
                        modifier = Modifier.padding(horizontal = 7.dp, vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        Icon(
                            painter = painterResource(PebbleIcons.PushPin),
                            contentDescription = null,
                            tint = accent,
                            modifier = Modifier.size(12.dp),
                        )
                        Text(
                            text = store.t("memPinnedTag"),
                            color = accent,
                            fontSize = PebbleFont.caption,
                            fontWeight = FontWeight.SemiBold,
                        )
                    }
                }
            }
            if (statusVal.isNotEmpty() && statusVal != "active") {
                val (bg, fg) = when (statusVal) {
                    "candidate" -> p.lilac to p.lilacInk
                    else -> p.s1 to p.mute
                }
                TagPill(
                    text = localizedMemoryStatus(statusVal, store),
                    bg = bg,
                    fg = fg,
                )
            }
            if (showScope && !item.scope.isNullOrBlank()) {
                val scopeLabel = when {
                    item.scope == "global" || item.scope == "everywhere" -> store.t("memScopeGlobal")
                    !item.botId.isNullOrBlank() -> "@${item.botId}"
                    else -> store.t("memScopeBot")
                }
                Text(scopeLabel, color = p.mute, fontSize = PebbleFont.caption, fontFamily = MonoFont)
            }
            if (showTags) {
                item.tags.orEmpty().filterNot { it.startsWith("bot:") }.forEach { tag ->
                    Text("#$tag", color = p.mute, fontSize = PebbleFont.caption)
                }
            }
        }

        if (!item.title.isNullOrBlank()) {
            Text(
                text = item.title,
                color = p.text,
                fontSize = PebbleFont.body,
                fontWeight = FontWeight.SemiBold,
            )
        }

        if (bodyText.isNotBlank()) {
            SelectionContainer {
                Text(bodyText, color = p.text, fontSize = PebbleFont.body)
            }
        }

        if (!item.why.isNullOrBlank()) {
            Text(
                text = store.t("memWhyPrefix", mapOf("why" to item.why)),
                color = p.text2,
                fontSize = PebbleFont.secondary,
            )
        }

        val metaParts = buildList {
            if (!item.validFrom.isNullOrBlank()) add(store.t("memValidFrom", mapOf("date" to item.validFrom)))
            if (!item.validUntil.isNullOrBlank()) add(store.t("memValidUntil", mapOf("date" to item.validUntil)))
            if (!item.supersededBy.isNullOrBlank()) add(store.t("memSupersededBy", mapOf("id" to item.supersededBy)))
            if (!item.sourceAuthor.isNullOrBlank()) add(item.sourceAuthor)
        }
        if (metaParts.isNotEmpty()) {
            Text(
                text = metaParts.joinToString(" · "),
                color = p.mute,
                fontSize = PebbleFont.caption,
            )
        }

        FlowRow(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            val compactPad = PaddingValues(horizontal = 8.dp, vertical = 2.dp)
            if (statusVal == "candidate") {
                if (onApprove != null) {
                    TextButton(onClick = onApprove, enabled = !busy, contentPadding = compactPad) {
                        Text(store.t("memApproveAction"), color = p.sageInk, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                    }
                }
                if (onDismiss != null) {
                    TextButton(onClick = onDismiss, enabled = !busy, contentPadding = compactPad) {
                        Text(store.t("memDismissAction"), color = p.danger, fontSize = PebbleFont.secondary)
                    }
                }
            }
            if (statusVal == "archived" && onRestore != null) {
                TextButton(onClick = onRestore, enabled = !busy, contentPadding = compactPad) {
                    Text(store.t("memRestoreAction"), color = accent, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                }
            }
            if (onTogglePin != null) {
                TextButton(onClick = onTogglePin, enabled = !busy, contentPadding = compactPad) {
                    Text(
                        text = store.t(if (item.pin == true) "memUnpinAction" else "memPinAction"),
                        color = if (item.pin == true) accent else p.text2,
                        fontSize = PebbleFont.secondary,
                    )
                }
            }
            if (onEdit != null) {
                TextButton(onClick = onEdit, enabled = !busy, contentPadding = compactPad) {
                    Text(store.t("edit"), color = p.text2, fontSize = PebbleFont.secondary)
                }
            }
            if (statusVal != "archived" && statusVal != "candidate" && onArchive != null) {
                TextButton(onClick = onArchive, enabled = !busy, contentPadding = compactPad) {
                    Text(store.t("memArchiveAction"), color = p.text2, fontSize = PebbleFont.secondary)
                }
            }
            if (onDelete != null) {
                TextButton(onClick = onDelete, enabled = !busy, contentPadding = compactPad) {
                    Text(store.t("delete"), color = p.danger, fontSize = PebbleFont.secondary)
                }
            }
        }
    }
}
