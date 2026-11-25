package ai.pebble.android.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.ui.bots.MemoryItemRow
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

private val MEM_KIND_KEYS = listOf("fact", "preference", "rule", "reference", "profile", "procedure", "episode")

@Composable
fun MemorySettingsTab(
    store: PebbleStore,
    router: PebbleRouter,
    onError: (String) -> Unit,
    onSaved: (String) -> Unit,
    onConfirm: (ConfirmItem) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var userMd by remember { mutableStateOf("") }
    var userMdTouched by remember { mutableStateOf(false) }
    var userMdSaving by remember { mutableStateOf(false) }

    var mem by remember { mutableStateOf<MemoryList?>(null) }
    var memScope by remember { mutableStateOf("global") }
    var memFilterKind by remember { mutableStateOf("") }
    var memQuery by remember { mutableStateOf("") }
    var memAdd by remember { mutableStateOf(false) }
    var memKind by remember { mutableStateOf("fact") }
    var memText by remember { mutableStateOf("") }
    var memTags by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf("") }

    fun syncUserMd() {
        if (!userMdTouched) {
            userMd = store.settings?.sharedUserMd.orEmpty()
        }
    }

    val userMdDirty = run {
        val s = store.settings ?: return@run false
        fun rtrim(x: String) = x.replace(Regex("\\s+$"), "")
        userMdTouched && rtrim(userMd) != rtrim(s.sharedUserMd.orEmpty())
    }

    suspend fun saveUserMd(): Boolean {
        onError("")
        onSaved("")
        userMdSaving = true
        return try {
            store.saveSharedUserMd(userMd)
            userMdTouched = false
            userMd = store.settings?.sharedUserMd.orEmpty()
            router.settingsDirty = false
            onSaved(store.t("settingsSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
        } finally {
            userMdSaving = false
        }
    }

    suspend fun loadMem(q: String = memQuery, scopeVal: String = memScope, kindVal: String = memFilterKind) {
        try {
            mem = store.fetchMemories(scope = scopeVal, query = q, kind = kindVal, limit = 30)
        } catch (e: Throwable) {
            onError(errText(e, store.t("opFailed")))
        }
    }

    fun runOp(key: String, failKey: String, op: suspend () -> String?) {
        onError("")
        onSaved("")
        busy = key
        scope.launch {
            try {
                op()?.let(onSaved)
            } catch (e: Throwable) {
                onError(errText(e, store.t(failKey)))
            } finally {
                busy = ""
            }
        }
    }

    LaunchedEffect(userMdDirty) {
        router.settingsDirty = userMdDirty
    }

    LaunchedEffect(store.settings?.sharedUserMd) {
        syncUserMd()
    }

    LaunchedEffect(store.isConnected) {
        if (store.isConnected) {
            runCatching { store.loadSettings() }
            syncUserMd()
            loadMem()
        }
    }

    DisposableEffect(Unit) {
        router.settingsSaveAction = { saveUserMd() }
        router.settingsDiscardAction = {
            userMdTouched = false
            userMd = store.settings?.sharedUserMd.orEmpty()
        }
        onDispose {
            router.settingsDirty = false
            router.settingsSaveAction = null
            router.settingsDiscardAction = null
        }
    }

    // Card 1: _shared/USER.md
    SectionHeader(store.t("globalSharedMem"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            OutlinedTextField(
                value = userMd,
                onValueChange = { next ->
                    if (next != userMd) {
                        userMd = next
                        userMdTouched = true
                        onSaved("")
                    }
                },
                placeholder = { Text(store.t("userMdPh")) },
                minLines = 4,
                maxLines = 16,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("_shared/USER.md", color = p.mute, fontSize = PebbleFont.secondary, fontFamily = MonoFont, modifier = Modifier.weight(1f))
                Button(
                    onClick = { scope.launch { saveUserMd() } },
                    enabled = userMdDirty && !userMdSaving && busy.isEmpty(),
                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                ) {
                    if (userMdSaving) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.onAccent)
                    } else {
                        Text(store.t("save"), fontSize = PebbleFont.secondary)
                    }
                }
            }
        }
    }

    // Card 2: Magpie Global Memory
    SectionHeader(store.t("magpieGlobalMem"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val sem = mem?.semantic
            if (mem?.installed == true && sem != null) {
                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    TagPill(
                        text = store.t(if (sem.active == true) "memSemanticOn" else "memSemanticOff"),
                        bg = if (sem.active == true) p.sage else p.s1,
                        fg = if (sem.active == true) p.sageInk else p.text2,
                    )
                    Spacer(Modifier.weight(1f))
                    if (sem.active != true) {
                        TextButton(onClick = { runOp("magpie:open", "opFailed") { store.openMagpie(); null } }) {
                            Text(store.t("openMagpie"), color = p.accent, fontSize = PebbleFont.secondary)
                        }
                    }
                }
                if (sem.active != true) {
                    Text(store.t("memSemanticHintOff"), color = p.text2, fontSize = PebbleFont.secondary)
                }
            }

            val scopes = listOf("global" to store.t("memScopeGlobal"), "all" to store.t("memScopeAll"))
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                scopes.forEachIndexed { idx, (sc, lbl) ->
                    SegmentedButton(
                        selected = memScope == sc,
                        onClick = {
                            memScope = sc
                            scope.launch { loadMem(memQuery, scopeVal = sc) }
                        },
                        shape = SegmentedButtonDefaults.itemShape(idx, scopes.size),
                        label = { Text(lbl, fontSize = PebbleFont.secondary) },
                    )
                }
            }

            var kindMenuOpen by remember { mutableStateOf(false) }
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
                            memFilterKind = ""
                            kindMenuOpen = false
                            scope.launch { loadMem(memQuery, kindVal = "") }
                        },
                    )
                    MEM_KIND_KEYS.forEach { k ->
                        DropdownMenuItem(
                            text = { Text(localizedMemoryKind(k, store), color = p.text, fontSize = PebbleFont.body) },
                            onClick = {
                                memFilterKind = k
                                kindMenuOpen = false
                                scope.launch { loadMem(memQuery, kindVal = k) }
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
                    onValueChange = { memQuery = it },
                    placeholder = {
                        Text(store.t(if (mem?.semantic?.active == true) "searchSemanticMemPh" else "searchGlobalMemPh"))
                    },
                    singleLine = true,
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.weight(1f),
                )
                if (memQuery.isNotEmpty()) {
                    IconButton(onClick = { memQuery = ""; scope.launch { loadMem("") } }) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("clear"), tint = p.mute)
                    }
                }
                IconButton(onClick = { scope.launch { loadMem(memQuery) } }) {
                    Icon(painterResource(PebbleIcons.Search), contentDescription = store.t("search"), tint = p.accent)
                }
            }

            val m = mem
            when {
                m?.installed != true -> {
                    Text(store.t("hostNoMagpieGlobal"), color = p.text2, fontSize = PebbleFont.secondary)
                    OutlinedButton(
                        onClick = {
                            runOp("magpie:install", "opFailed") {
                                store.installMagpie()
                                loadMem(memQuery)
                                store.t("mpInstallDone")
                            }
                        },
                        enabled = busy.isEmpty(),
                    ) {
                        if (busy == "magpie:install") {
                            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.accent)
                            Spacer(Modifier.width(8.dp))
                            Text(store.t("mpInstalling"), color = p.text, fontSize = PebbleFont.secondary)
                        } else {
                            Text(store.t("mpInstallOnHost"), color = p.text, fontSize = PebbleFont.secondary)
                        }
                    }
                }
                m.available != true -> {
                    val detailErr = m.error?.takeIf { it.isNotEmpty() }?.let { ": $it" }.orEmpty()
                    Text(store.t("magpieNoResp") + detailErr, color = p.danger, fontSize = PebbleFont.secondary)
                }
                m.items.isNullOrEmpty() -> {
                    Text(store.t("noGlobalMem"), color = p.mute, fontSize = PebbleFont.secondary)
                }
                else -> {
                    m.items.forEachIndexed { idx, it ->
                        if (idx > 0) CardDivider()
                        MemoryItemRow(
                            store = store,
                            item = it,
                            showTags = true,
                            showScope = memScope == "all",
                            onDelete = {
                                onConfirm(
                                    ConfirmItem(
                                        title = store.t("deleteMemTitle"),
                                        message = store.t("deleteMemConfirm"),
                                    ) {
                                        scope.launch {
                                            runCatching {
                                                store.deleteMemory(it.id)
                                                loadMem(memQuery)
                                            }.onFailure { err -> onError(errText(err, store.t("delFailed"))) }
                                        }
                                    },
                                )
                            },
                        )
                    }
                }
            }

            CardDivider()
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { memAdd = !memAdd }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("addShort"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                Icon(painterResource(if (memAdd) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
            }

            AnimatedVisibility(visible = memAdd) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    var addKindOpen by remember { mutableStateOf(false) }
                    Box {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .clickable { addKindOpen = true }
                                .padding(vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(store.t("memType"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                            Text(localizedMemoryKind(memKind, store), color = p.text, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                            Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                        }
                        DropdownMenu(expanded = addKindOpen, onDismissRequest = { addKindOpen = false }, containerColor = p.card) {
                            MEM_KIND_KEYS.forEach { k ->
                                DropdownMenuItem(
                                    text = { Text(localizedMemoryKind(k, store), color = p.text, fontSize = PebbleFont.body) },
                                    trailingIcon = if (memKind == k) {
                                        { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                                    } else {
                                        null
                                    },
                                    onClick = { memKind = k; addKindOpen = false },
                                )
                            }
                        }
                    }

                    OutlinedTextField(
                        value = memText,
                        onValueChange = { memText = it },
                        placeholder = { Text(store.t("globalMemPh")) },
                        minLines = 2,
                        maxLines = 6,
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = memTags,
                        onValueChange = { memTags = it },
                        placeholder = { Text(store.t("tagsPh")) },
                        singleLine = true,
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Button(
                        onClick = {
                            val txt = memText.trim()
                            if (txt.isEmpty()) {
                                onError(store.t("memContentReq"))
                                return@Button
                            }
                            val tags = memTags.split(',', '，').map { it.trim() }.filter { it.isNotEmpty() } // i18n:allow-cjk
                            runOp("mem:save", "saveMemFailed") {
                                store.saveMemory(mapOf("scope" to "global", "kind" to memKind, "text" to txt, "tags" to tags))
                                memText = ""
                                memTags = ""
                                memAdd = false
                                loadMem(memQuery)
                                store.t("globalMemSaved")
                            }
                        },
                        enabled = busy != "mem:save",
                        colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                    ) {
                        Text(store.t("saveMemory"), fontSize = PebbleFont.secondary)
                    }
                }
            }
        }
    }
}
