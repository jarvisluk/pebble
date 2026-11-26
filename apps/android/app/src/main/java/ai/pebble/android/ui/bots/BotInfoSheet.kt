package ai.pebble.android.ui.bots

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HubException
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.RoutineItem
import ai.pebble.android.data.RoutineRun
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.settings.ModelInputWithMenu
import ai.pebble.android.ui.settings.localizedMemoryKind
import ai.pebble.android.ui.settings.settingsHarnessList
import ai.pebble.android.ui.desktop.H264SurfaceView
import ai.pebble.android.ui.theme.BotAppearancePicker
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.copyToClipboard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons


@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BotInfoSheet(
    store: PebbleStore,
    botId: String,
    onClose: () -> Unit,
    onDeletedBot: () -> Unit,
    onOpenFiles: (String) -> Unit = {},
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var detail by remember(botId) { mutableStateOf<BotStatus?>(null) }
    var loading by remember(botId) { mutableStateOf(false) }
    var appearOpen by remember(botId) { mutableStateOf(false) }
    var editName by remember(botId) { mutableStateOf("") }
    var editDesc by remember(botId) { mutableStateOf("") }
    var editAvatar by remember(botId) { mutableStateOf("") }
    var editColor by remember(botId) { mutableStateOf("") }
    var editInst by remember(botId) { mutableStateOf("") }
    var editHarness by remember(botId) { mutableStateOf("") }
    var editModel by remember(botId) { mutableStateOf("") }
    var editPrivate by remember(botId) { mutableStateOf(false) }
    var editDesktopTarget by remember(botId) { mutableStateOf("") }
    var editBrowserMode by remember(botId) { mutableStateOf("") }
    var editPermMode by remember(botId) { mutableStateOf("auto") }
    var saving by remember(botId) { mutableStateOf(false) }
    var err by remember(botId) { mutableStateOf("") }
    var confirm by remember(botId) { mutableStateOf<ConfirmItem?>(null) }

    var mem by remember(botId) { mutableStateOf<MemoryList?>(null) }
    var memLoading by remember(botId) { mutableStateOf(false) }
    var memText by remember(botId) { mutableStateOf("") }
    var memSaving by remember(botId) { mutableStateOf(false) }

    val index = store.bots.indexOfFirst { it.id == botId }
    val bot = if (index >= 0) store.bots[index] else null
    val active = detail ?: bot
    val pal = p.bot(botId, index, editColor.ifEmpty { active?.color })

    val canBindDesktop = store.supports("bot.desktop_binding")
    val canIsolateBrowser = store.supports("bot.browser_isolation")

    val harnesses = remember(store.settings?.harnesses, editHarness) {
        val list = settingsHarnessList(store)
        if (editHarness.isNotEmpty() && list.none { it.id == editHarness }) {
            list + ai.pebble.android.data.HarnessInfo(id = editHarness, label = editHarness)
        } else {
            list
        }
    }

    val effectiveModelPlaceholder = remember(editHarness, store.settings, store.langRevision) {
        val per = store.settings?.defaultModels?.get(editHarness).orEmpty().trim()
        val ph = per.ifEmpty { resolveDefaultModel(store.settings?.defaultModel.orEmpty().trim(), editHarness) }
        ph.ifEmpty { store.t("inheritDefaultPh") }
    }

    val dirty = remember(active, detail, editName, editDesc, editAvatar, editColor, editHarness, editModel, editPrivate, editDesktopTarget, editBrowserMode, editPermMode, editInst, canBindDesktop, canIsolateBrowser) {
        val activePerm = store.clampPermissionMode(active?.permissionMode, active?.harness)
        val baseModel = (if (active?.modelInherited == true) "" else active?.model.orEmpty()).trim()
        active != null && (
            editName != (active.name ?: botId) ||
                editDesc != active.description.orEmpty() ||
                editAvatar != active.avatar.orEmpty() ||
                editColor != active.color.orEmpty() ||
                editHarness != active.harness.orEmpty() ||
                editModel.trim() != baseModel ||
                editPrivate != (active.privateSessions == true) ||
                (canBindDesktop && editDesktopTarget != active.desktopTarget.orEmpty()) ||
                (canIsolateBrowser && editBrowserMode != active.browserMode.orEmpty()) ||
                editPermMode != activePerm ||
                editInst != detail?.instructions.orEmpty()
            )
    }

    val desktopTargetOptions = remember(store.desktopTargets, botId, editDesktopTarget) {
        val opts = mutableListOf(
            "" to store.t("deskBindDefault"),
            "local" to store.t("localDesktopName"),
        )
        val seen = linkedSetOf("", "local")
        if (store.desktopTargets.firstOrNull { it.id == "local" }?.virtualSupported == true) {
            val vid = "virtual:$botId"
            seen.add(vid)
            opts.add(vid to store.t("deskBindLocalVirtual"))
        }
        store.desktopTargets.filter { it.id != "local" }.forEach { tr ->
            if (seen.add(tr.id)) {
                opts.add(tr.id to (tr.name ?: tr.id))
            }
            if (tr.virtual != true && tr.virtualSupported == true) {
                val rvid = "${tr.id}:virtual:$botId"
                if (seen.add(rvid)) {
                    opts.add(rvid to store.t("deskBindRemoteVirtual", mapOf("name" to (tr.name ?: tr.id))))
                }
            }
        }
        if (editDesktopTarget.isNotEmpty() && seen.add(editDesktopTarget)) {
            opts.add(editDesktopTarget to store.t("deskBindOfflineTag", mapOf("id" to editDesktopTarget)))
        }
        opts
    }

    val browserModeOptions = remember(store.langPref) {
        listOf(
            "" to store.t("browserModeAuto"),
            "isolated" to store.t("browserModeIsolated"),
            "attach" to store.t("browserModeAttach"),
        )
    }

    fun apply(d: BotStatus) {
        detail = d
        editName = d.name ?: botId
        editDesc = d.description.orEmpty()
        editAvatar = d.avatar.orEmpty()
        editColor = d.color.orEmpty()
        editHarness = d.harness.orEmpty()
        editModel = (if (d.modelInherited == true) "" else d.model.orEmpty()).trim()
        editInst = d.instructions.orEmpty()
        editPrivate = d.privateSessions == true
        editDesktopTarget = d.desktopTarget.orEmpty()
        editBrowserMode = d.browserMode.orEmpty()
        editPermMode = store.clampPermissionMode(d.permissionMode, d.harness)
        if (editHarness.isNotEmpty()) {
            scope.launch { runCatching { store.loadModels(editHarness) } }
        }
    }

    suspend fun loadMem() {
        memLoading = true
        runCatching { store.fetchMemories(scope = "project", botId = botId, limit = 30) }
            .getOrNull()?.let { mem = it }
        memLoading = false
    }

    LaunchedEffect(botId) {
        val src = detail ?: bot
        editName = src?.name ?: botId
        editDesc = src?.description.orEmpty()
        editAvatar = src?.avatar.orEmpty()
        editColor = src?.color.orEmpty()
        editHarness = src?.harness.orEmpty()
        editModel = (if (src?.modelInherited == true) "" else src?.model.orEmpty()).trim()
        editInst = detail?.instructions.orEmpty()
        editPrivate = src?.privateSessions == true
        editDesktopTarget = src?.desktopTarget.orEmpty()
        editBrowserMode = src?.browserMode.orEmpty()
        editPermMode = store.clampPermissionMode(src?.permissionMode, src?.harness)
        loading = true
        scope.launch { loadMem() }
        scope.launch { runCatching { store.refreshDesktopTargets() } }
        scope.launch {
            if (store.settings == null) runCatching { store.loadSettings() }
            if (editHarness.isNotEmpty()) runCatching { store.loadModels(editHarness) }
        }
        runCatching { store.botDetail(botId) }.getOrNull()?.let { apply(it) }
        loading = false
    }

    fun save() {
        val n = editName.trim()
        if (n.isEmpty()) {
            err = store.t("nameRequired")
            return
        }
        err = ""
        saving = true
        scope.launch {
            try {
                val payload = mutableMapOf<String, Any?>(
                    "name" to n,
                    "description" to editDesc.trim(),
                    "avatar" to editAvatar,
                    "color" to editColor,
                    "harness" to editHarness,
                    "model" to editModel.trim(),
                    "instructions" to editInst,
                    "privateSessions" to editPrivate,
                    "permissionMode" to editPermMode,
                )
                if (canBindDesktop) payload["desktopTarget"] = editDesktopTarget
                if (canIsolateBrowser) payload["browserMode"] = editBrowserMode
                store.updateBot(botId, payload)
                apply(store.botDetail(botId))
            } catch (e: Throwable) {
                err = errText(e, store.t("saveFailed"))
            } finally {
                saving = false
            }
        }
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    Scaffold(
        containerColor = p.page,
        topBar = {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.page,
                    titleContentColor = p.text,
                ),
                navigationIcon = {
                    IconButton(onClick = onClose) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                    }
                },
                title = {
                    Text(store.t("botDetails"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
                actions = {
                    if (saving) {
                        CircularProgressIndicator(modifier = Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp, color = pal.accent)
                    } else {
                        TextButton(
                            onClick = { save() },
                            enabled = dirty && editName.trim().isNotEmpty(),
                        ) {
                            Text(
                                text = store.t("save"),
                                color = if (dirty && editName.trim().isNotEmpty()) pal.accent else p.mute,
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                },
            )
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .imePadding()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (err.isNotEmpty()) {
                SectionCard {
                    Box(modifier = Modifier.padding(14.dp)) {
                        StatusTextBanner(text = err, error = true)
                    }
                }
            }

            // Profile + Stats Card
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        BotAvatar(
                            id = botId,
                            size = 56.dp,
                            index = index,
                            avatar = editAvatar,
                            color = editColor,
                            interactive = true,
                        )
                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            OutlinedTextField(
                                value = editName,
                                onValueChange = { editName = it },
                                placeholder = { Text(store.t("botNamePh")) },
                                singleLine = true,
                                textStyle = TextStyle(fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold),
                                shape = PebbleDimens.rowShape,
                                colors = pebbleFieldColors(),
                                modifier = Modifier.fillMaxWidth(),
                            )
                            Text("@$botId", color = p.mute, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
                        }
                    }

                    OutlinedTextField(
                        value = editDesc,
                        onValueChange = { editDesc = it },
                        placeholder = { Text(store.t("descPh")) },
                        minLines = 1,
                        maxLines = 4,
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )

                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { appearOpen = !appearOpen }
                            .padding(horizontal = 4.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("appearance"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                        Icon(painterResource(if (appearOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                            contentDescription = null,
                            tint = p.mute,
                        )
                    }

                    AnimatedVisibility(visible = appearOpen) {
                        BotAppearancePicker(
                            botId = botId,
                            index = index,
                            avatar = editAvatar,
                            color = editColor,
                            accent = pal.accent,
                            onAvatarChange = { editAvatar = it },
                            onColorChange = { editColor = it },
                        )
                    }

                    CardDivider()
                    DetailRow(store.t("statTurns"), "${active?.session?.turnCount ?: 0}")
                    CardDivider()
                    DetailRow(store.t("statCost"), String.format(Locale.US, "$%.4f", active?.session?.totalCostUsd ?: 0.0))
                }
            }

            // Bot Memory
            SectionHeader(store.t("botMemory"))
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    val m = mem
                    when {
                        memLoading && m == null -> CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                        m?.installed != true -> {
                            Text(store.t("hostNoMagpie"), color = p.text2, fontSize = PebbleFont.body)
                            TextButton(
                                onClick = {
                                    scope.launch {
                                        runCatching {
                                            store.installMagpie()
                                            loadMem()
                                        }.onFailure { err = errText(it, store.t("opFailed")) }
                                    }
                                },
                                contentPadding = PaddingValues(0.dp),
                            ) {
                                Text(store.t("mpInstallOnHost"), color = pal.accent, fontSize = PebbleFont.body)
                            }
                        }
                        m.available != true -> {
                            Text(store.t("magpieNoResp"), color = p.danger, fontSize = PebbleFont.body)
                        }
                        else -> {
                            val items = m.items.orEmpty()
                            if (items.isEmpty()) {
                                Text(store.t("noBotMemories"), color = p.mute, fontSize = PebbleFont.secondary)
                            } else {
                                items.forEachIndexed { idx, item ->
                                    if (idx > 0) CardDivider()
                                    MemoryItemRow(
                                        store = store,
                                        item = item,
                                        onDelete = {
                                            confirm = ConfirmItem(
                                                title = store.t("deleteMemTitle"),
                                                message = store.t("deleteMemConfirm"),
                                            ) {
                                                scope.launch {
                                                    runCatching { store.deleteMemory(item.id) }
                                                    loadMem()
                                                }
                                            }
                                        },
                                    )
                                }
                            }
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.Bottom,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                OutlinedTextField(
                                    value = memText,
                                    onValueChange = { memText = it },
                                    placeholder = { Text(store.t("botMemPh")) },
                                    minLines = 1,
                                    maxLines = 4,
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.weight(1f),
                                )
                                Button(
                                    onClick = {
                                        val txt = memText.trim()
                                        if (txt.isEmpty()) return@Button
                                        memSaving = true
                                        scope.launch {
                                            try {
                                                store.saveMemory(
                                                    mapOf(
                                                        "scope" to "project",
                                                        "botId" to botId,
                                                        "kind" to "fact",
                                                        "text" to txt,
                                                    ),
                                                )
                                                memText = ""
                                                loadMem()
                                            } catch (e: Throwable) {
                                                err = errText(e, store.t("saveBotMemFailed"))
                                            } finally {
                                                memSaving = false
                                            }
                                        }
                                    },
                                    enabled = !memSaving && memText.trim().isNotEmpty(),
                                    colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                                ) {
                                    Text(store.t("saveMemory"), fontSize = PebbleFont.secondary)
                                }
                            }
                        }
                    }
                }
            }

            BotTechDetailsCard(
                store = store,
                botId = botId,
                pal = pal,
                active = active,
                detail = detail,
                loading = loading,
                harnesses = harnesses,
                effectiveModelPlaceholder = effectiveModelPlaceholder,
                canBindDesktop = canBindDesktop,
                canIsolateBrowser = canIsolateBrowser,
                desktopTargetOptions = desktopTargetOptions,
                browserModeOptions = browserModeOptions,
                editHarness = editHarness,
                onEditHarnessChange = { editHarness = it },
                editModel = editModel,
                onEditModelChange = { editModel = it },
                editPermMode = editPermMode,
                onEditPermModeChange = { editPermMode = it },
                editDesktopTarget = editDesktopTarget,
                onEditDesktopTargetChange = { editDesktopTarget = it },
                editBrowserMode = editBrowserMode,
                onEditBrowserModeChange = { editBrowserMode = it },
                editPrivate = editPrivate,
                onEditPrivateChange = { editPrivate = it },
                editInst = editInst,
                onEditInstChange = { editInst = it },
                onClose = onClose,
                onOpenFiles = onOpenFiles,
            )

            // Danger actions
            SectionCard {
                Column(modifier = Modifier.fillMaxWidth()) {
                    Text(
                        text = store.t("resetSessionBtn"),
                        color = p.text,
                        fontSize = PebbleFont.cardTitle,
                        fontWeight = FontWeight.Medium,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                confirm = ConfirmItem(
                                    title = store.t("resetSessionTitle"),
                                    message = store.t("resetSessionDesc", mapOf("name" to (bot?.displayName ?: botId))),
                                ) {
                                    scope.launch {
                                        try {
                                            store.resetBot(botId)
                                            onClose()
                                            runCatching { store.loadMessages(botId) }
                                        } catch (e: Throwable) {
                                            err = errText(e, store.t("resetFailed"))
                                        }
                                    }
                                }
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                    )
                    if (store.bots.size > 1) {
                        CardDivider()
                        Text(
                            text = store.t("deleteBotBtn"),
                            color = p.danger,
                            fontSize = PebbleFont.cardTitle,
                            fontWeight = FontWeight.Medium,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    confirm = ConfirmItem(
                                        title = store.t("deleteBotTitle", mapOf("name" to (bot?.displayName ?: botId))),
                                        message = store.t("deleteBotDesc"),
                                    ) {
                                        scope.launch {
                                            try {
                                                store.deleteBot(botId)
                                                onClose()
                                                onDeletedBot()
                                            } catch (e: Throwable) {
                                                err = errText(e, store.t("delFailed"))
                                            }
                                        }
                                    }
                                }
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                        )
                    }
                }
            }

            Spacer(Modifier.height(16.dp))
        }
    }
}
