package ai.pebble.android.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.DesktopPermissionItem
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionFooter
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.launch

@Composable
fun DesktopSettingsTab(
    store: PebbleStore,
    router: PebbleRouter,
    onOpenDesktop: (String) -> Unit,
    onError: (String) -> Unit,
    onSaved: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    val harnesses = installedHarnessList(store)

    var permTarget by remember { mutableStateOf("local") }
    var targetMenuOpen by remember { mutableStateOf(false) }
    var deskPerms by remember { mutableStateOf<List<DesktopPermissionItem>>(emptyList()) }
    var deskPermsLoading by remember { mutableStateOf(false) }
    var permBusyId by remember { mutableStateOf("") }

    var deskHarness by remember { mutableStateOf("") }
    var deskModel by remember { mutableStateOf("") }
    var deskTouched by remember { mutableStateOf(false) }
    var deskSaving by remember { mutableStateOf(false) }
    var loadingModelsFor by remember { mutableStateOf(setOf<String>()) }

    val nonVirtualTargets = remember(store.desktopTargets) {
        store.desktopTargets.filter { it.virtual != true }
    }
    val targetOptions = remember(nonVirtualTargets, store.langRevision) {
        if (nonVirtualTargets.isEmpty()) {
            listOf("local" to store.t("localDesktopName"))
        } else {
            nonVirtualTargets.map { tr ->
                val lbl = if (tr.id == "local") store.t("localDesktopName") else (tr.name?.takeIf { it.isNotEmpty() } ?: tr.id)
                tr.id to lbl
            }
        }
    }

    val localEngineText = remember(store.desktopTargets, store.isConnected, store.langRevision) {
        val local = store.desktopTargets.firstOrNull { it.id == "local" }
        when {
            !local?.backend.isNullOrEmpty() -> local?.backend.orEmpty()
            local?.supported == false -> store.t("unavailable")
            store.isConnected -> "WebRTC / H.265·H.264"
            else -> store.t("checking")
        }
    }

    fun syncWorker() {
        if (!deskTouched) {
            deskHarness = store.settings?.desktopHarness.orEmpty()
            deskModel = store.settings?.desktopModel.orEmpty()
        }
    }

    val deskDirty = run {
        val s = store.settings ?: return@run false
        deskTouched && (deskHarness != s.desktopHarness.orEmpty() || deskModel.trim() != s.desktopModel.orEmpty().trim())
    }

    suspend fun loadPerms(target: String = permTarget) {
        deskPermsLoading = true
        runCatching { store.fetchDesktopPermissions(target) }.getOrNull()?.items?.let { deskPerms = it }
        deskPermsLoading = false
    }

    fun loadCands(harnessId: String, force: Boolean = false) {
        if (harnessId.isEmpty()) return
        if (!force && !store.models[harnessId].isNullOrEmpty()) return
        loadingModelsFor = loadingModelsFor + harnessId
        scope.launch {
            try {
                store.loadModels(harnessId, force = force)
            } catch (e: Throwable) {
                if (force) onError(errText(e, store.t("loadModelsFailed")))
            } finally {
                loadingModelsFor = loadingModelsFor - harnessId
            }
        }
    }

    suspend fun saveWorker(): Boolean {
        onError("")
        onSaved("")
        deskSaving = true
        return try {
            store.setDesktopWorker(deskHarness, deskModel)
            deskTouched = false
            deskHarness = store.settings?.desktopHarness.orEmpty()
            deskModel = store.settings?.desktopModel.orEmpty()
            router.settingsDirty = false
            onSaved(store.t("deskWorkerSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
        } finally {
            deskSaving = false
        }
    }

    LaunchedEffect(deskDirty) {
        router.settingsDirty = deskDirty
    }

    LaunchedEffect(store.settings?.desktopHarness, store.settings?.desktopModel) {
        syncWorker()
    }

    LaunchedEffect(store.isConnected) {
        if (store.isConnected) {
            runCatching { store.loadSettings() }
            runCatching { store.refreshDesktopTargets() }
            if (targetOptions.none { it.first == permTarget }) {
                permTarget = targetOptions.firstOrNull()?.first ?: "local"
            }
            syncWorker()
            loadPerms(permTarget)
        }
    }

    DisposableEffect(Unit) {
        router.settingsSaveAction = { saveWorker() }
        router.settingsDiscardAction = {
            deskTouched = false
            deskHarness = store.settings?.desktopHarness.orEmpty()
            deskModel = store.settings?.desktopModel.orEmpty()
        }
        onDispose {
            router.settingsDirty = false
            router.settingsSaveAction = null
            router.settingsDiscardAction = null
        }
    }

    // Card 1: Desktop Permissions
    SectionHeader(
        title = store.t("deskPermsTitle"),
        trailing = {
            IconButton(
                onClick = { scope.launch { loadPerms(permTarget) } },
                enabled = !deskPermsLoading,
                modifier = Modifier.size(28.dp),
            ) {
                Icon(painterResource(PebbleIcons.Refresh), contentDescription = store.t("refresh"), tint = p.accent, modifier = Modifier.size(18.dp))
            }
        },
    )
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (targetOptions.size > 1) {
                Box {
                    val curLabel = targetOptions.firstOrNull { it.first == permTarget }?.second ?: permTarget
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { targetMenuOpen = true }
                            .padding(vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("deskPermTarget"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                        Text(curLabel, color = p.text, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                        Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                    }
                    DropdownMenu(expanded = targetMenuOpen, onDismissRequest = { targetMenuOpen = false }, containerColor = p.card) {
                        targetOptions.forEach { (id, lbl) ->
                            DropdownMenuItem(
                                text = { Text(lbl, color = p.text, fontSize = PebbleFont.body) },
                                trailingIcon = if (id == permTarget) {
                                    { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                                } else {
                                    null
                                },
                                onClick = {
                                    targetMenuOpen = false
                                    if (id != permTarget) {
                                        permTarget = id
                                        scope.launch { loadPerms(id) }
                                    }
                                },
                            )
                        }
                    }
                }
                CardDivider()
            }

            if (deskPerms.isEmpty()) {
                if (deskPermsLoading) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                } else {
                    Text(store.t("deskPermsDesc"), color = p.text2, fontSize = PebbleFont.secondary)
                }
            } else {
                deskPerms.forEachIndexed { idx, item ->
                    if (idx > 0) CardDivider()
                    val stLabel = when (item.status) {
                        "granted" -> store.t("deskPermGranted")
                        "denied" -> store.t("deskPermDenied")
                        "unsupported" -> store.t("deskPermUnsupported")
                        else -> store.t("deskPermNotDetermined")
                    }
                    val bg = when (item.status) {
                        "granted" -> p.sage
                        "denied" -> p.pink
                        else -> p.s1
                    }
                    val fg = when (item.status) {
                        "granted" -> p.sageInk
                        "denied" -> p.danger
                        else -> p.text2
                    }
                    val permName = store.t("deskPermName_${item.id}").let { if (it == "deskPermName_${item.id}") item.name else it }
                    val permDesc = store.t("deskPermDesc_${item.id}").let { if (it == "deskPermDesc_${item.id}") item.description else it }
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                            Text(permName, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            TagPill(stLabel, bg, fg)
                        }
                        Text(permDesc, color = p.text2, fontSize = PebbleFont.secondary)
                        if ((item.canPrompt == true && item.status != "granted") || item.canOpenSettings == true) {
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                if (item.canPrompt == true && item.status != "granted") {
                                    Button(
                                        onClick = {
                                            permBusyId = item.id
                                            onError("")
                                            onSaved("")
                                            scope.launch {
                                                try {
                                                    val res = store.requestDesktopPermission(permTarget, item.id, openSettings = false)
                                                    res.items?.let { deskPerms = it }
                                                    if (res.openedSettings == true) onSaved(store.t("deskPermOpened"))
                                                    else if (res.prompted == true) onSaved(store.t("deskPermPrompted"))
                                                } catch (e: Throwable) {
                                                    onError(errText(e, store.t("opFailed")))
                                                } finally {
                                                    permBusyId = ""
                                                }
                                            }
                                        },
                                        enabled = permBusyId != item.id,
                                        colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                    ) {
                                        Text(store.t("deskPermRequest"), fontSize = PebbleFont.secondary)
                                    }
                                }
                                if (item.canOpenSettings == true) {
                                    OutlinedButton(
                                        onClick = {
                                            permBusyId = item.id
                                            onError("")
                                            onSaved("")
                                            scope.launch {
                                                try {
                                                    val res = store.requestDesktopPermission(permTarget, item.id, openSettings = true)
                                                    res.items?.let { deskPerms = it }
                                                    if (res.openedSettings == true) onSaved(store.t("deskPermOpened"))
                                                } catch (e: Throwable) {
                                                    onError(errText(e, store.t("opFailed")))
                                                } finally {
                                                    permBusyId = ""
                                                }
                                            }
                                        },
                                        enabled = permBusyId != item.id,
                                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                    ) {
                                        Text(store.t("deskPermOpenSettings"), fontSize = PebbleFont.secondary, color = p.text)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            CardDivider()
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { onOpenDesktop(permTarget) }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(painterResource(PebbleIcons.DesktopWindows), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                Text(store.t("openDesktop"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
            }
        }
    }
    SectionFooter(store.t("deskPermsDesc"))

    // Card 2: Desktop Worker
    val defHarness = store.settings?.defaultHarness?.takeIf { it.isNotEmpty() } ?: (harnesses.firstOrNull()?.id.orEmpty())
    val orderedGlobal = orderedGlobalHarnesses(harnesses, defHarness, store.models)

    SectionHeader(store.t("deskWorkerTitle"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("localDesktopEngine"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                Text(localEngineText, color = p.text, fontSize = PebbleFont.secondary)
            }
            CardDivider()

            var dwHarnessMenu by remember { mutableStateOf(false) }
            Box {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { dwHarnessMenu = true }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(store.t("deskWorkerHarnessLabel"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                    Text(
                        text = if (deskHarness.isEmpty()) store.t("deskWorkerFollowBot") else harnessLabelForSelection(deskHarness, store),
                        color = p.text,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                    )
                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                }
                DropdownMenu(expanded = dwHarnessMenu, onDismissRequest = { dwHarnessMenu = false }, containerColor = p.card) {
                    DropdownMenuItem(
                        text = { Text(store.t("deskWorkerFollowBot"), color = p.text, fontSize = PebbleFont.body) },
                        onClick = { deskHarness = ""; deskTouched = true; dwHarnessMenu = false },
                    )
                    harnesses.forEach { h ->
                        DropdownMenuItem(
                            text = { Text(h.title, color = p.text, fontSize = PebbleFont.body) },
                            onClick = {
                                deskHarness = h.id
                                deskTouched = true
                                dwHarnessMenu = false
                                loadCands(h.id)
                            },
                        )
                    }
                }
            }

            if (deskHarness.isEmpty()) {
                GlobalModelInputWithMenu(
                    value = deskModel,
                    placeholder = store.t("deskWorkerModelPh"),
                    harnesses = orderedGlobal,
                    defaultHarness = defHarness,
                    harnessLabel = { harnessDisplayLabel(it, store) },
                    modelsByHarness = store.models,
                    loadingHarnesses = loadingModelsFor,
                    onLoadHarness = { loadCands(it) },
                    onRefreshHarness = { loadCands(it, force = true) },
                    onRefreshAll = { harnesses.forEach { loadCands(it.id, force = true) } },
                    onValueChange = { deskModel = it; deskTouched = true; onSaved("") },
                    store = store,
                )
            } else {
                ModelInputWithMenu(
                    value = deskModel,
                    placeholder = store.t("deskWorkerModelPh"),
                    candidates = store.models[deskHarness].orEmpty(),
                    loading = deskHarness in loadingModelsFor,
                    onLoadCandidates = { loadCands(deskHarness) },
                    onRefreshCandidates = { loadCands(deskHarness, force = true) },
                    onValueChange = { deskModel = it; deskTouched = true; onSaved("") },
                    store = store,
                )
            }

            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                Button(
                    onClick = { scope.launch { saveWorker() } },
                    enabled = deskDirty && !deskSaving,
                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                ) {
                    if (deskSaving) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.onAccent)
                    } else {
                        Text(store.t("save"), fontSize = PebbleFont.secondary)
                    }
                }
            }
        }
    }
}
