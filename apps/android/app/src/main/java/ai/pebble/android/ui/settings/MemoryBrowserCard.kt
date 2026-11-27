package ai.pebble.android.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.pebbleFieldColors

private val SETTINGS_STATUS_FILTERS = listOf("active", "candidate", "superseded", "archived", "all")

@Composable
internal fun MemoryBrowserCard(
    store: PebbleStore,
    mem: MemoryList?,
    candidateCount: Int,
    memScope: String,
    memStatusFilter: String,
    memFilterKind: String,
    memQuery: String,
    memAdd: Boolean,
    addSeq: Int,
    editingId: String?,
    busy: String,
    onScopeChange: (String) -> Unit,
    onStatusChange: (String) -> Unit,
    onKindChange: (String) -> Unit,
    onQueryChange: (String) -> Unit,
    onSearch: (String) -> Unit,
    onToggleAdd: () -> Unit,
    onStartEdit: (String?) -> Unit,
    onAddSubmit: (MemoryFormInput) -> Unit,
    onEditSubmit: (MemoryItem, MemoryFormInput) -> Unit,
    onTogglePin: (MemoryItem) -> Unit,
    onApprove: (MemoryItem) -> Unit,
    onDismiss: (MemoryItem) -> Unit,
    onArchive: (MemoryItem) -> Unit,
    onRestore: (MemoryItem) -> Unit,
    onDeleteConfirm: (MemoryItem) -> Unit,
) {
    val p = LocalPalette.current
    var kindMenuOpen by remember { mutableStateOf(false) }

    SectionHeader(store.t("memItemsTitle"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val scopes = listOf("global" to store.t("memScopeGlobal"), "all" to store.t("memScopeAll"))
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                scopes.forEachIndexed { idx, (sc, lbl) ->
                    SegmentedButton(
                        selected = memScope == sc,
                        onClick = { onScopeChange(sc) },
                        shape = SegmentedButtonDefaults.itemShape(idx, scopes.size),
                        label = {
                            Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        },
                    )
                }
            }

            MemoryStatusFilterRow(
                store = store,
                statuses = SETTINGS_STATUS_FILTERS,
                selected = memStatusFilter,
                candidateCount = candidateCount,
                accent = p.accent,
                onAccent = p.onAccent,
                onSelect = onStatusChange,
            )

            Box {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { kindMenuOpen = true }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(store.t("memType"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                    Text(
                        text = if (memFilterKind.isEmpty()) store.t("memKindAll") else localizedMemoryKind(memFilterKind, store),
                        color = p.text,
                        fontSize = PebbleFont.secondary,
                    )
                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                }
                DropdownMenu(expanded = kindMenuOpen, onDismissRequest = { kindMenuOpen = false }, containerColor = p.card) {
                    DropdownMenuItem(
                        text = { Text(store.t("memKindAll"), color = p.text, fontSize = PebbleFont.body) },
                        onClick = {
                            kindMenuOpen = false
                            onKindChange("")
                        },
                    )
                    MEMORY_KINDS.forEach { k ->
                        DropdownMenuItem(
                            text = { Text(localizedMemoryKind(k, store), color = p.text, fontSize = PebbleFont.body) },
                            onClick = {
                                kindMenuOpen = false
                                onKindChange(k)
                            },
                        )
                    }
                }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                OutlinedTextField(
                    value = memQuery,
                    onValueChange = onQueryChange,
                    placeholder = {
                        Text(store.t(if (mem?.semantic?.active == true) "searchSemanticMemPh" else "searchGlobalMemPh"))
                    },
                    singleLine = true,
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.weight(1f),
                )
                if (memQuery.isNotEmpty()) {
                    IconButton(onClick = { onQueryChange(""); onSearch("") }) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("clear"), tint = p.mute)
                    }
                }
                IconButton(onClick = { onSearch(memQuery) }) {
                    Icon(painterResource(PebbleIcons.Search), contentDescription = store.t("search"), tint = p.accent)
                }
            }

            when {
                mem != null && mem.available == false -> {
                    val detailErr = mem.error?.takeIf { it.isNotEmpty() }?.let { ": $it" }.orEmpty()
                    Text(store.t("memUnavailable") + detailErr, color = p.danger, fontSize = PebbleFont.secondary)
                }
                mem == null || mem.items.isNullOrEmpty() -> {
                    val emptyKey = if (memQuery.isNotBlank() || memFilterKind.isNotEmpty() || memStatusFilter != "active") {
                        "memNoMatching"
                    } else {
                        "noGlobalMem"
                    }
                    Text(store.t(emptyKey), color = p.mute, fontSize = PebbleFont.secondary)
                }
                else -> {
                    mem.items.forEachIndexed { idx, item ->
                        if (idx > 0) CardDivider()
                        if (editingId == item.id) {
                            MemoryEditorForm(
                                store = store,
                                textPlaceholder = store.t("globalMemPh"),
                                accent = p.accent,
                                onAccent = p.onAccent,
                                saving = busy == "mem:edit:${item.id}",
                                initial = item,
                                onCancel = { onStartEdit(null) },
                                onSubmit = { form -> onEditSubmit(item, form) },
                            )
                        } else {
                            MemoryItemCardRow(
                                store = store,
                                item = item,
                                accent = p.accent,
                                showTags = true,
                                showScope = memScope == "all",
                                busy = busy.isNotEmpty(),
                                onTogglePin = { onTogglePin(item) },
                                onEdit = { onStartEdit(item.id) },
                                onApprove = { onApprove(item) },
                                onDismiss = { onDismiss(item) },
                                onArchive = { onArchive(item) },
                                onRestore = { onRestore(item) },
                                onDelete = { onDeleteConfirm(item) },
                            )
                        }
                    }
                }
            }

            CardDivider()
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { onToggleAdd() }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = store.t("addShort"),
                    color = p.accent,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    painter = painterResource(if (memAdd) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                    contentDescription = null,
                    tint = p.mute,
                )
            }

            AnimatedVisibility(visible = memAdd) {
                key(addSeq) {
                    MemoryEditorForm(
                        store = store,
                        textPlaceholder = store.t("globalMemPh"),
                        accent = p.accent,
                        onAccent = p.onAccent,
                        saving = busy == "mem:save",
                        onSubmit = onAddSubmit,
                    )
                }
            }
        }
    }
}
