package ai.pebble.android.ui.bots

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
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
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.settings.ModelInputWithMenu
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

@Composable
internal fun BotTechDetailsCard(
    store: PebbleStore,
    botId: String,
    pal: BotAccent,
    active: BotStatus?,
    detail: BotStatus?,
    loading: Boolean,
    harnesses: List<HarnessInfo>,
    effectiveModelPlaceholder: String,
    canBindDesktop: Boolean,
    canIsolateBrowser: Boolean,
    desktopTargetOptions: List<Pair<String, String>>,
    browserModeOptions: List<Pair<String, String>>,
    editHarness: String,
    onEditHarnessChange: (String) -> Unit,
    editModel: String,
    onEditModelChange: (String) -> Unit,
    editPermMode: String,
    onEditPermModeChange: (String) -> Unit,
    editDesktopTarget: String,
    onEditDesktopTargetChange: (String) -> Unit,
    editBrowserMode: String,
    onEditBrowserModeChange: (String) -> Unit,
    editPrivate: Boolean,
    onEditPrivateChange: (Boolean) -> Unit,
    editInst: String,
    onEditInstChange: (String) -> Unit,
    onClose: () -> Unit,
    onOpenFiles: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    var techOpen by remember(botId) { mutableStateOf(false) }
    var harnessMenuOpen by remember(botId) { mutableStateOf(false) }
    var loadingModels by remember(botId) { mutableStateOf(false) }
    var screenMenuOpen by remember(botId) { mutableStateOf(false) }
    var browserMenuOpen by remember(botId) { mutableStateOf(false) }
    var permMenuOpen by remember(botId) { mutableStateOf(false) }

    SectionCard {
        Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { techOpen = !techOpen }
                    .padding(horizontal = 4.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("techDetails"), color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                Icon(painterResource(if (techOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
            }
            AnimatedVisibility(visible = techOpen) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Box(modifier = Modifier.fillMaxWidth()) {
                        val currentHarnessLabel = ai.pebble.android.ui.settings.harnessLabelForSelection(editHarness, store)
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .clickable(enabled = harnesses.isNotEmpty()) { harnessMenuOpen = true }
                                .padding(vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Text(store.t("statHarness"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                            Text(currentHarnessLabel, color = if (harnesses.isEmpty()) p.mute else p.text2, fontSize = PebbleFont.secondary)
                            if (harnesses.isNotEmpty()) {
                                Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                            }
                        }
                        DropdownMenu(
                            expanded = harnessMenuOpen,
                            onDismissRequest = { harnessMenuOpen = false },
                            containerColor = p.card,
                        ) {
                            harnesses.forEach { h ->
                                DropdownMenuItem(
                                    text = { Text(h.title, color = p.text, fontSize = PebbleFont.body) },
                                    trailingIcon = if (editHarness == h.id) {
                                        { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp)) }
                                    } else {
                                        null
                                    },
                                    onClick = {
                                        harnessMenuOpen = false
                                        if (h.id != editHarness) {
                                            onEditHarnessChange(h.id)
                                            onEditModelChange("")
                                            onEditPermModeChange(store.clampPermissionMode(editPermMode, h.id))
                                            scope.launch { runCatching { store.loadModels(h.id) } }
                                        }
                                    },
                                )
                            }
                        }
                    }
                    Text(store.t("statModel"), color = p.text2, fontSize = PebbleFont.secondary)
                    ModelInputWithMenu(
                        value = editModel,
                        placeholder = effectiveModelPlaceholder,
                        candidates = store.models[editHarness].orEmpty(),
                        loading = loadingModels,
                        onLoadCandidates = {
                            if (editHarness.isNotEmpty() && !loadingModels) {
                                loadingModels = true
                                scope.launch {
                                    try {
                                        store.loadModels(editHarness)
                                    } finally {
                                        loadingModels = false
                                    }
                                }
                            }
                        },
                        onRefreshCandidates = {
                            if (editHarness.isNotEmpty() && !loadingModels) {
                                loadingModels = true
                                scope.launch {
                                    try {
                                        store.loadModels(editHarness, force = true)
                                    } finally {
                                        loadingModels = false
                                    }
                                }
                            }
                        },
                        onValueChange = { onEditModelChange(it) },
                        store = store,
                    )
                    val permModes = store.supportedPermissionModes(editHarness)
                    if (permModes.size <= 1) {
                        val onlyMode = permModes.firstOrNull() ?: "auto"
                        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            DetailRow(store.t("permMode"), store.t("permMode_$onlyMode"))
                            Text(
                                store.t("permModeUnsupported", mapOf("mode" to store.t("permMode_$onlyMode"))),
                                color = p.mute,
                                fontSize = PebbleFont.caption,
                            )
                        }
                    } else {
                        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Box(modifier = Modifier.fillMaxWidth()) {
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable { permMenuOpen = true }
                                        .padding(vertical = 8.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Text(store.t("permMode"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                    Text(
                                        text = store.t("permMode_$editPermMode"),
                                        color = p.text2,
                                        fontSize = PebbleFont.secondary,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                    Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                                }
                                DropdownMenu(
                                    expanded = permMenuOpen,
                                    onDismissRequest = { permMenuOpen = false },
                                    containerColor = p.card,
                                ) {
                                    permModes.forEach { m ->
                                        DropdownMenuItem(
                                            text = { Text(store.t("permMode_$m"), color = p.text, fontSize = PebbleFont.body) },
                                            trailingIcon = {
                                                if (editPermMode == m) {
                                                    Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                                }
                                            },
                                            onClick = {
                                                onEditPermModeChange(m)
                                                permMenuOpen = false
                                            },
                                        )
                                    }
                                }
                            }
                            Text(store.t("permDesc_$editPermMode"), color = p.mute, fontSize = PebbleFont.caption)
                        }
                    }
                    val archivedText = store.t("archivedCount", mapOf("n" to (active?.archivedSessions ?: 0))) +
                        if (active?.privateSessions == true) store.t("privateTag") else ""
                    DetailRow(store.t("statArchived"), archivedText)
                    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text(store.t("workspaceDir"), color = p.text2, fontSize = PebbleFont.secondary)
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .clickable {
                                    onClose()
                                    onOpenFiles(botId)
                                }
                                .padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            Text(
                                text = active?.workspace ?: "-",
                                color = pal.accent,
                                fontSize = PebbleFont.secondary,
                                fontFamily = MonoFont,
                                maxLines = 1,
                                overflow = TextOverflow.MiddleEllipsis,
                                modifier = Modifier.weight(1f),
                            )
                            Icon(
                                painter = painterResource( PebbleIcons.Folder),
                                contentDescription = store.t("workspaceFiles"),
                                tint = pal.accent,
                                modifier = Modifier.size(16.dp),
                            )
                        }
                    }
                    if (canBindDesktop) {
                        Box(modifier = Modifier.fillMaxWidth()) {
                            val currentScreenLabel = desktopTargetOptions.firstOrNull { it.first == editDesktopTarget }?.second ?: editDesktopTarget
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(PebbleDimens.rowShape)
                                    .clickable { screenMenuOpen = true }
                                    .padding(vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Text(store.t("botDesktopTarget"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(
                                    text = currentScreenLabel,
                                    color = p.text2,
                                    fontSize = PebbleFont.secondary,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.widthIn(max = 170.dp),
                                )
                                Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                            }
                            DropdownMenu(
                                expanded = screenMenuOpen,
                                onDismissRequest = { screenMenuOpen = false },
                                containerColor = p.card,
                            ) {
                                desktopTargetOptions.forEach { (id, label) ->
                                    DropdownMenuItem(
                                        text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                                        trailingIcon = {
                                            if (editDesktopTarget == id) {
                                                Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                            }
                                        },
                                        onClick = {
                                            onEditDesktopTargetChange(id)
                                            screenMenuOpen = false
                                        },
                                    )
                                }
                            }
                        }
                    }
                    if (canIsolateBrowser) {
                        Box(modifier = Modifier.fillMaxWidth()) {
                            val currentBrowserLabel = browserModeOptions.firstOrNull { it.first == editBrowserMode }?.second ?: editBrowserMode
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(PebbleDimens.rowShape)
                                    .clickable { browserMenuOpen = true }
                                    .padding(vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Text(store.t("botBrowserMode"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(
                                    text = currentBrowserLabel,
                                    color = p.text2,
                                    fontSize = PebbleFont.secondary,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.widthIn(max = 170.dp),
                                )
                                Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                            }
                            DropdownMenu(
                                expanded = browserMenuOpen,
                                onDismissRequest = { browserMenuOpen = false },
                                containerColor = p.card,
                            ) {
                                browserModeOptions.forEach { (id, label) ->
                                    DropdownMenuItem(
                                        text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                                        trailingIcon = {
                                            if (editBrowserMode == id) {
                                                Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                            }
                                        },
                                        onClick = {
                                            onEditBrowserModeChange(id)
                                            browserMenuOpen = false
                                        },
                                    )
                                }
                            }
                        }
                    }
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("privateSessions"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                        Switch(checked = editPrivate, onCheckedChange = { onEditPrivateChange(it) })
                    }
                    Text("AGENTS.md", color = p.text2, fontSize = PebbleFont.secondary)
                    if (loading && detail == null) {
                        CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                    } else {
                        OutlinedTextField(
                            value = editInst,
                            onValueChange = { onEditInstChange(it) },
                            placeholder = { Text(store.t("agentsPh"), fontFamily = MonoFont) },
                            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                            minLines = 5,
                            maxLines = 16,
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }
        }
    }
}
