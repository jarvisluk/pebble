package ai.pebble.android.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.pebbleFieldColors

fun settingsHarnessList(store: PebbleStore): List<HarnessInfo> {
    val list = store.settings?.harnesses.orEmpty()
    return list.ifEmpty {
        listOf("opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy")
            .map { HarnessInfo(id = it, label = it) }
    }
}

fun harnessDisplayLabel(h: HarnessInfo, store: PebbleStore): String = when {
    h.magpie?.connected == true -> "${h.title} · Magpie"
    store.settings?.magpie?.installed == true -> "${h.title} · ${store.t("mpNative")}"
    else -> h.title
}

fun orderedGlobalHarnesses(
    harnesses: List<HarnessInfo>,
    defaultHarness: String,
    models: Map<String, List<String>>,
): List<HarnessInfo> = buildList {
    val seen = mutableSetOf<String>()
    val anyInstalled = harnesses.any { it.installed == true }
    fun addUnique(h: HarnessInfo) {
        if (seen.add(h.id)) add(h)
    }
    harnesses.firstOrNull { it.id == defaultHarness }?.let(::addUnique)
    harnesses.filter { (!anyInstalled || it.installed == true) && it.magpie?.connected != true }.forEach(::addUnique)
    harnesses.filter { (!anyInstalled || it.installed == true) && it.magpie?.connected == true }.forEach(::addUnique)
    harnesses.filter { !anyInstalled || !models[it.id].isNullOrEmpty() }.forEach(::addUnique)
}

fun localizedMemoryKind(raw: String?, store: PebbleStore): String {
    val k = raw.orEmpty().trim()
    return when (k) {
        "fact" -> store.t("kindFact")
        "preference" -> store.t("kindPreference")
        "rule" -> store.t("kindRule")
        "reference" -> store.t("kindReference")
        "profile" -> store.t("kindProfile")
        "procedure" -> store.t("kindProcedure")
        "episode" -> store.t("kindEpisode")
        else -> k.ifEmpty { "memory" }
    }
}

@Composable
fun ModelInputWithMenu(
    value: String,
    placeholder: String,
    candidates: List<String>,
    loading: Boolean,
    onLoadCandidates: () -> Unit,
    onRefreshCandidates: () -> Unit = onLoadCandidates,
    onValueChange: (String) -> Unit,
    store: PebbleStore,
) {
    val p = LocalPalette.current
    var menuOpen by remember { mutableStateOf(false) }
    val trimmedValue = value.trim()

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            placeholder = { Text(placeholder, fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.weight(1f),
        )
        Box {
            IconButton(onClick = {
                menuOpen = true
                if (candidates.isEmpty() && !loading) onLoadCandidates()
            }) {
                Icon(painterResource(PebbleIcons.List), contentDescription = store.t("optionalModels"), tint = p.accent)
            }
            DropdownMenu(
                expanded = menuOpen,
                onDismissRequest = { menuOpen = false },
                containerColor = p.card,
                modifier = Modifier.heightIn(max = 360.dp),
            ) {
                if (trimmedValue.isNotEmpty()) {
                    DropdownMenuItem(
                        text = { Text(store.t("clear"), color = p.danger, fontSize = PebbleFont.secondary) },
                        onClick = { onValueChange(""); menuOpen = false },
                    )
                    HorizontalDivider(color = p.s2)
                }
                if (candidates.isEmpty()) {
                    DropdownMenuItem(
                        text = { Text(if (loading) store.t("loading") else store.t("noModels"), color = p.mute, fontSize = PebbleFont.secondary) },
                        enabled = false,
                        onClick = {},
                    )
                    if (!loading) {
                        DropdownMenuItem(
                            text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                            onClick = onRefreshCandidates,
                        )
                    }
                } else {
                    candidates.forEach { m ->
                        val selected = m == trimmedValue
                        DropdownMenuItem(
                            text = {
                                Text(
                                    text = m,
                                    fontFamily = MonoFont,
                                    fontSize = PebbleFont.secondary,
                                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                                    color = if (selected) p.accent else p.text,
                                )
                            },
                            trailingIcon = if (selected) {
                                { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                            } else {
                                null
                            },
                            onClick = { onValueChange(m); menuOpen = false },
                        )
                    }
                    HorizontalDivider(color = p.s2)
                    DropdownMenuItem(
                        text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                        onClick = onRefreshCandidates,
                    )
                }
            }
        }
    }
}

@Composable
fun GlobalModelInputWithMenu(
    value: String,
    placeholder: String,
    harnesses: List<HarnessInfo>,
    defaultHarness: String,
    harnessLabel: (HarnessInfo) -> String,
    modelsByHarness: Map<String, List<String>>,
    loadingHarnesses: Set<String>,
    onLoadHarness: (String) -> Unit,
    onRefreshHarness: (String) -> Unit,
    onRefreshAll: () -> Unit,
    onValueChange: (String) -> Unit,
    store: PebbleStore,
) {
    val p = LocalPalette.current
    var menuOpen by remember { mutableStateOf(false) }
    var activeHarnessId by remember { mutableStateOf<String?>(null) }
    val trimmedValue = value.trim()

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            placeholder = { Text(placeholder, fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.weight(1f),
        )
        Box {
            IconButton(onClick = {
                val singleId = if (harnesses.size == 1) harnesses.first().id else null
                activeHarnessId = singleId
                menuOpen = true
                if (singleId != null && modelsByHarness[singleId].isNullOrEmpty() && singleId !in loadingHarnesses) {
                    onLoadHarness(singleId)
                }
            }) {
                Icon(painterResource(PebbleIcons.List), contentDescription = store.t("optionalModels"), tint = p.accent)
            }
            DropdownMenu(
                expanded = menuOpen,
                onDismissRequest = { menuOpen = false; activeHarnessId = null },
                containerColor = p.card,
                modifier = Modifier.heightIn(max = 360.dp),
            ) {
                if (trimmedValue.isNotEmpty()) {
                    DropdownMenuItem(
                        text = { Text(store.t("clear"), color = p.danger, fontSize = PebbleFont.secondary) },
                        onClick = { onValueChange(""); menuOpen = false; activeHarnessId = null },
                    )
                    HorizontalDivider(color = p.s2)
                }
                val selectedId = activeHarnessId
                if (selectedId == null) {
                    harnesses.forEach { h ->
                        val count = modelsByHarness[h.id]?.size ?: 0
                        val isLoading = h.id in loadingHarnesses
                        val lbl = if (h.id == defaultHarness) "${harnessLabel(h)} (${store.t("defaultTag")})" else harnessLabel(h)
                        DropdownMenuItem(
                            text = { Text(lbl, color = p.text, fontSize = PebbleFont.secondary) },
                            trailingIcon = {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                                    Text(
                                        text = if (isLoading && count == 0) store.t("loading") else "$count",
                                        color = p.mute,
                                        fontSize = PebbleFont.caption,
                                    )
                                    Icon(painterResource(PebbleIcons.ChevronRight), contentDescription = null, tint = p.mute, modifier = Modifier.size(16.dp))
                                }
                            },
                            onClick = {
                                activeHarnessId = h.id
                                if (modelsByHarness[h.id].isNullOrEmpty() && h.id !in loadingHarnesses) {
                                    onLoadHarness(h.id)
                                }
                            },
                        )
                    }
                    HorizontalDivider(color = p.s2)
                    DropdownMenuItem(
                        text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                        onClick = onRefreshAll,
                    )
                } else {
                    val activeInfo = harnesses.firstOrNull { it.id == selectedId }
                    if (harnesses.size > 1) {
                        DropdownMenuItem(
                            text = { Text(activeInfo?.let(harnessLabel) ?: selectedId, color = p.text, fontWeight = FontWeight.SemiBold, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = null, tint = p.text, modifier = Modifier.size(18.dp)) },
                            onClick = { activeHarnessId = null },
                        )
                        HorizontalDivider(color = p.s2)
                    }
                    val cands = modelsByHarness[selectedId].orEmpty()
                    val isLoading = selectedId in loadingHarnesses
                    if (cands.isEmpty()) {
                        DropdownMenuItem(
                            text = { Text(if (isLoading) store.t("loading") else store.t("noModels"), color = p.mute, fontSize = PebbleFont.secondary) },
                            enabled = false,
                            onClick = {},
                        )
                        if (!isLoading) {
                            DropdownMenuItem(
                                text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                                leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                                onClick = { onRefreshHarness(selectedId) },
                            )
                        }
                    } else {
                        cands.forEach { m ->
                            val selected = m == trimmedValue
                            DropdownMenuItem(
                                text = {
                                    Text(
                                        text = m,
                                        fontFamily = MonoFont,
                                        fontSize = PebbleFont.secondary,
                                        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                                        color = if (selected) p.accent else p.text,
                                    )
                                },
                                trailingIcon = if (selected) {
                                    { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                                } else {
                                    null
                                },
                                onClick = { onValueChange(m); menuOpen = false; activeHarnessId = null },
                            )
                        }
                        HorizontalDivider(color = p.s2)
                        DropdownMenuItem(
                            text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                            onClick = { onRefreshHarness(selectedId) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
internal fun PebbleSwitch(
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    enabled: Boolean = true,
) {
    val p = LocalPalette.current
    Switch(
        checked = checked,
        onCheckedChange = onCheckedChange,
        enabled = enabled,
        colors = SwitchDefaults.colors(
            checkedThumbColor = Color.White,
            checkedTrackColor = p.sageInk,
            uncheckedThumbColor = Color.White,
            uncheckedTrackColor = p.s3,
            uncheckedBorderColor = Color.Transparent,
        ),
    )
}
