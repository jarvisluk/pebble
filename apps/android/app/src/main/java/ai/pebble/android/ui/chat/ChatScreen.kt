package ai.pebble.android.ui.chat

import android.content.Context
import android.graphics.BitmapFactory
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.ChatMessage
import ai.pebble.android.data.MentionCandidate
import ai.pebble.android.data.MessageMedia
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.PendingAttachment
import ai.pebble.android.data.SlashCommandItem
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatStamp
import ai.pebble.android.data.parseIsoInstant
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Duration
import java.util.UUID
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

private sealed interface TimelineEntry {
    val id: String
    val createdAt: String?

    data class Message(val msg: ChatMessage) : TimelineEntry {
        override val id: String get() = msg.id
        override val createdAt: String? get() = msg.createdAt
    }

    data class Collab(
        override val id: String,
        val peerId: String,
        val items: List<ChatMessage>,
        override val createdAt: String?,
    ) : TimelineEntry
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    store: PebbleStore,
    botId: String,
    compact: Boolean,
    inDesktopSidebar: Boolean = false,
    onBack: (() -> Unit)? = null,
    onOpenDesktop: (String) -> Unit = {},
    onOpenFiles: (String) -> Unit = {},
    onToggleDetail: (() -> Unit)? = null,
    onOpenBotInfo: (() -> Unit)? = null,
    onSelectBot: ((String) -> Unit)? = null,
    onCloseSidebar: (() -> Unit)? = null,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val listState = rememberLazyListState()

    var input by remember(botId) { mutableStateOf("") }
    var sending by remember(botId) { mutableStateOf(false) }
    var sendErr by remember(botId) { mutableStateOf("") }
    var collabPeerId by remember(botId) { mutableStateOf<String?>(null) }
    var previewMedia by remember(botId) { mutableStateOf<MessageMedia?>(null) }
    var commands by remember(botId) { mutableStateOf<List<SlashCommandItem>>(emptyList()) }
    var mentions by remember(botId) { mutableStateOf<List<MentionCandidate>>(emptyList()) }
    var mentionJob by remember(botId) { mutableStateOf<Job?>(null) }
    var acDismissed by remember(botId) { mutableStateOf(false) }
    val pendingAttachments = remember(botId) { mutableStateListOf<PendingAttachment>() }
    var attachMenuOpen by remember { mutableStateOf(false) }
    var botSwitcherOpen by remember { mutableStateOf(false) }

    val index = store.bots.indexOfFirst { it.id == botId }
    val bot = if (index >= 0) store.bots[index] else null
    val botName = bot?.displayName ?: botId
    val pal = p.bot(botId, index, bot?.color)
    val messages = store.msgs[botId].orEmpty()
    val botQuestions = store.questions.filter { it.botId == botId }
    val showStop = (bot?.isBusy == true) && input.trim().isEmpty()

    val timelineEntries = remember(messages) {
        val entries = mutableListOf<TimelineEntry>()
        for (m in messages) {
            if (m.role == "bot_out" || m.role == "bot_in") {
                val rawPeer = if (m.role == "bot_out") m.to.orEmpty() else m.from.orEmpty()
                val peerId = rawPeer.removePrefix("bot:")
                val lastEntry = entries.lastOrNull()
                if (lastEntry is TimelineEntry.Collab && lastEntry.peerId == peerId) {
                    entries[entries.lastIndex] = lastEntry.copy(items = lastEntry.items + m)
                } else {
                    entries.add(TimelineEntry.Collab("collab_${m.id}", peerId, listOf(m), m.createdAt))
                }
            } else {
                entries.add(TimelineEntry.Message(m))
            }
        }
        entries
    }

    LaunchedEffect(botId) {
        store.setActiveChat(botId)
        runCatching { store.loadMessages(botId) }
        commands = runCatching { store.fetchCommands(botId) }.getOrDefault(emptyList())
    }

    LaunchedEffect(store.pendingChatInsert?.seq) {
        val insert = store.pendingChatInsert ?: return@LaunchedEffect
        if (insert.text.isEmpty()) return@LaunchedEffect
        val needsSpace = input.isNotEmpty() && !input.last().isWhitespace()
        input += (if (needsSpace) " " else "") + insert.text
    }

    DisposableEffect(botId) {
        onDispose {
            store.setActiveChat(null)
        }
    }

    LaunchedEffect(timelineEntries.size, botQuestions.size, bot?.isBusy) {
        if (timelineEntries.isNotEmpty() || botQuestions.isNotEmpty()) {
            val total = timelineEntries.size + botQuestions.size + 1
            runCatching { listState.animateScrollToItem(total) }
        }
    }

    fun uploadUri(uri: Uri, forceImage: Boolean) {
        scope.launch {
            val info = withContext(Dispatchers.IO) { readUriPayload(context, uri, forceImage) } ?: return@launch
            val localId = UUID.randomUUID().toString()
            pendingAttachments.add(
                PendingAttachment(
                    id = localId,
                    name = info.name,
                    isImage = info.isImage,
                    previewBytes = if (info.isImage) info.bytes else null,
                    uploading = true,
                ),
            )
            try {
                val res = store.uploadAttachment(botId, info.bytes, info.name, info.mime)
                val idx = pendingAttachments.indexOfFirst { it.id == localId }
                if (idx >= 0) {
                    pendingAttachments[idx] = pendingAttachments[idx].copy(media = res.media, uploading = false)
                }
            } catch (e: Throwable) {
                val idx = pendingAttachments.indexOfFirst { it.id == localId }
                if (idx >= 0) {
                    pendingAttachments[idx] = pendingAttachments[idx].copy(
                        uploading = false,
                        error = errText(e, store.t("uploadFailed")),
                    )
                }
            }
        }
    }

    val photoPicker = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(5)) { uris ->
        uris.forEach { uploadUri(it, forceImage = true) }
    }

    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        uris.forEach { uploadUri(it, forceImage = false) }
    }

    val slashQuery = remember(input, acDismissed) {
        if (acDismissed || !input.startsWith("/")) null
        else {
            val rest = input.drop(1)
            if (rest.any { it.isWhitespace() }) null else rest
        }
    }

    val filteredCommands = remember(slashQuery, commands) {
        val q = slashQuery?.lowercase() ?: return@remember emptyList()
        if (q.isEmpty()) commands else commands.filter {
            it.name.lowercase().contains(q) || it.description.orEmpty().lowercase().contains(q)
        }
    }

    val activeMentionRange = remember(input, acDismissed, slashQuery) {
        if (acDismissed || slashQuery != null) null
        else {
            val atIdx = input.lastIndexOf('@')
            if (atIdx < 0) null
            else if (atIdx > 0 && !input[atIdx - 1].isWhitespace()) null
            else {
                val after = input.substring(atIdx + 1)
                if (after.any { it.isWhitespace() }) null else (atIdx..input.length to after)
            }
        }
    }

    fun onInputChange(next: String) {
        acDismissed = false
        input = if (next.length > 8000) next.take(8000) else next
        mentionJob?.cancel()
        val atIdx = input.lastIndexOf('@')
        val validMention = !acDismissed && !input.startsWith("/") && atIdx >= 0 &&
            (atIdx == 0 || input[atIdx - 1].isWhitespace()) &&
            !input.substring(atIdx + 1).any { it.isWhitespace() }
        if (validMention) {
            val q = input.substring(atIdx + 1)
            mentionJob = scope.launch {
                delay(120)
                mentions = runCatching { store.fetchMentions(botId, q) }.getOrDefault(emptyList())
            }
        } else {
            mentions = emptyList()
        }
    }

    fun submit() {
        val text = input
        val readyMediaIds = pendingAttachments.mapNotNull { if (!it.uploading && it.error == null) it.media?.id else null }
        val trimmed = text.trim()
        if ((trimmed.isEmpty() && readyMediaIds.isEmpty()) || sending) return
        input = ""
        pendingAttachments.clear()
        sendErr = ""
        sending = true
        scope.launch {
            try {
                store.sendMessage(botId, trimmed, readyMediaIds)
            } catch (e: Throwable) {
                input = text
                sendErr = errText(e, store.t("sendFailed"))
            } finally {
                sending = false
            }
        }
    }

    fun interrupt() {
        scope.launch {
            runCatching { store.interruptBot(botId) }.onFailure {
                sendErr = errText(it, store.t("stopFailed"))
            }
        }
    }

    previewMedia?.let { media ->
        MediaPreviewDialog(store = store, media = media, onDismiss = { previewMedia = null })
    }

    collabPeerId?.let { peerId ->
        ModalBottomSheet(
            onDismissRequest = { collabPeerId = null },
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
            containerColor = p.page,
        ) {
            Box(modifier = Modifier.heightIn(min = 360.dp, max = 640.dp)) {
                CollabSheetView(
                    store = store,
                    botId = botId,
                    peerId = peerId,
                    onClose = { collabPeerId = null },
                )
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(p.page),
    ) {
        TopAppBar(
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = p.page,
                titleContentColor = p.text,
            ),
            navigationIcon = {
                if (compact && !inDesktopSidebar && onBack != null) {
                    IconButton(onClick = onBack) {
                        Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("back"), tint = p.text)
                    }
                }
            },
            title = {
                Box {
                    Row(
                        modifier = Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .then(
                                when {
                                    inDesktopSidebar && store.bots.size > 1 -> Modifier.clickable { botSwitcherOpen = true }
                                    !inDesktopSidebar && onOpenBotInfo != null -> Modifier.clickable { onOpenBotInfo() }
                                    else -> Modifier
                                },
                            )
                            .padding(vertical = 4.dp, horizontal = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        BotAvatar(
                            id = botId,
                            size = 28.dp,
                            index = index,
                            avatar = bot?.avatar,
                            color = bot?.color,
                            busy = bot?.isBusy == true,
                            interactive = true,
                        )
                        Text(
                            text = botName,
                            fontSize = PebbleFont.title,
                            fontWeight = FontWeight.Bold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f, fill = false),
                        )
                        if (inDesktopSidebar && store.bots.size > 1) {
                            Icon(painterResource(PebbleIcons.KeyboardArrowDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                        }
                    }

                    DropdownMenu(
                        expanded = botSwitcherOpen,
                        onDismissRequest = { botSwitcherOpen = false },
                        containerColor = p.card,
                    ) {
                        store.bots.forEach { b ->
                            DropdownMenuItem(
                                text = { Text(b.displayName, color = p.text) },
                                trailingIcon = {
                                    if (b.id == botId) {
                                        Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                    }
                                },
                                onClick = {
                                    botSwitcherOpen = false
                                    onSelectBot?.invoke(b.id)
                                },
                            )
                        }
                    }
                }
            },
            actions = {
                if (inDesktopSidebar) {
                    if (onCloseSidebar != null) {
                        IconButton(onClick = onCloseSidebar) {
                            Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                        }
                    }
                } else {
                    IconButton(onClick = { onOpenFiles(botId) }) {
                        Icon(
                            painter = painterResource( PebbleIcons.Folder),
                            contentDescription = store.t("workspaceFiles"),
                            tint = p.text,
                        )
                    }
                    IconButton(onClick = { onOpenDesktop(store.takeovers.firstOrNull()?.target ?: "local") }) {
                        Icon(
                            painter = painterResource(if (store.takeovers.isEmpty()) PebbleIcons.DesktopWindows else PebbleIcons.Warning),
                            contentDescription = store.t("remoteDesktop"),
                            tint = if (store.takeovers.isEmpty()) p.text else p.busy,
                        )
                    }
                    if (onToggleDetail != null) {
                        IconButton(onClick = onToggleDetail) {
                            Icon(painterResource(PebbleIcons.FormatListBulleted), contentDescription = store.t("rightPanelTitle"), tint = p.text)
                        }
                    }
                }
            },
        )

        // Status banners
        if (!inDesktopSidebar && store.usingStandby) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(p.s1)
                    .padding(horizontal = 16.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(painterResource(PebbleIcons.SyncProblem), contentDescription = null, tint = p.text2, modifier = Modifier.size(16.dp))
                Text(store.t("standbyActiveBanner"), color = p.text, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                TextButton(
                    onClick = { scope.launch { store.reconnect() } },
                    contentPadding = PaddingValues(horizontal = 8.dp),
                ) {
                    Text(store.t("retryPrimary"), fontSize = PebbleFont.secondary, color = p.accent)
                }
            }
        }
        if (!inDesktopSidebar && store.activeDesktopTarget.temporary) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(p.s1)
                    .padding(horizontal = 16.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Icon(painterResource(PebbleIcons.Computer), contentDescription = null, tint = p.text2, modifier = Modifier.size(16.dp))
                Text(
                    text = store.t(
                        "tempDesktopBanner",
                        mapOf("name" to (store.activeDesktopTarget.name ?: store.activeDesktopTarget.target)),
                    ),
                    color = p.text,
                    fontSize = PebbleFont.secondary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                TextButton(
                    onClick = { onOpenDesktop(store.activeDesktopTarget.target) },
                    contentPadding = PaddingValues(horizontal = 6.dp),
                ) {
                    Text(store.t("desktopShort"), fontSize = PebbleFont.secondary, color = p.accent)
                }
                TextButton(
                    onClick = { scope.launch { runCatching { store.setActiveDesktopTarget("local") } } },
                    contentPadding = PaddingValues(horizontal = 6.dp),
                ) {
                    Text(store.t("switchBackLocal"), fontSize = PebbleFont.secondary, color = p.accent)
                }
            }
        }
        val to = store.takeovers.firstOrNull()
        if (to != null) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(p.pink)
                    .padding(horizontal = 16.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(painterResource(PebbleIcons.PanTool), contentDescription = null, tint = p.pinkInk, modifier = Modifier.size(16.dp))
                Text(
                    text = store.t("takeoverInline", mapOf("reason" to to.reason.orEmpty())),
                    color = p.pinkInk,
                    fontSize = PebbleFont.body,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                OutlinedButton(
                    onClick = { onOpenDesktop(to.target ?: "local") },
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                ) {
                    Text(store.t("handleAction"), fontSize = PebbleFont.secondary, color = p.pinkInk)
                }
                Button(
                    onClick = { scope.launch { runCatching { store.resolveTakeover(to.id) } } },
                    colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                ) {
                    Text(store.t("resumeShort"), fontSize = PebbleFont.secondary)
                }
            }
        }

        // Message List + Floating Composer overlaid cleanly without black bottom cutoff
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars)),
        ) {
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(
                    start = if (compact) 10.dp else 16.dp,
                    end = if (compact) 10.dp else 16.dp,
                    top = 12.dp,
                    bottom = 86.dp,
                ),
                verticalArrangement = Arrangement.spacedBy(10.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                if (messages.isEmpty()) {
                    item(key = "empty_state") {
                        Column(
                            modifier = Modifier
                                .widthIn(max = 840.dp)
                                .fillMaxWidth()
                                .padding(top = 40.dp, bottom = 20.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            BotAvatar(
                                id = botId,
                                size = 72.dp,
                                index = index,
                                avatar = bot?.avatar,
                                color = bot?.color,
                                interactive = true,
                            )
                            Text(botName, color = p.text, fontSize = PebbleFont.hero, fontWeight = FontWeight.Bold)
                            Text(
                                text = bot?.description?.takeIf { it.isNotEmpty() } ?: store.t("emptyChatHint"),
                                color = p.text2,
                                fontSize = PebbleFont.body,
                                textAlign = TextAlign.Center,
                            )
                        }
                    }
                }

                itemsIndexed(timelineEntries, key = { _, e -> e.id }) { i, entry ->
                    val prevAt = if (i > 0) timelineEntries[i - 1].createdAt else null
                    val showStamp = shouldShowStamp(entry.createdAt, prevAt)
                    Column(
                        modifier = Modifier
                            .widthIn(max = 840.dp)
                            .fillMaxWidth(),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        if (showStamp) {
                            Text(
                                text = formatStamp(entry.createdAt),
                                color = p.mute,
                                fontSize = PebbleFont.secondary,
                                textAlign = TextAlign.Center,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(top = 4.dp),
                            )
                        }
                        when (entry) {
                            is TimelineEntry.Collab -> {
                                val peerIdx = store.bots.indexOfFirst { it.id == entry.peerId }
                                val peerBot = if (peerIdx >= 0) store.bots[peerIdx] else null
                                val peerName = peerBot?.displayName ?: entry.peerId
                                val peerPal = p.bot(entry.peerId, peerIdx.coerceAtLeast(0), peerBot?.color)
                                Surface(
                                    color = peerPal.collabBg,
                                    shape = CircleShape,
                                    modifier = Modifier
                                        .clip(CircleShape)
                                        .clickable {
                                            if (entry.peerId.isNotEmpty()) collabPeerId = entry.peerId
                                        },
                                ) {
                                    Row(
                                        modifier = Modifier.padding(horizontal = 12.dp, vertical = 7.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        BotAvatar(
                                            id = entry.peerId,
                                            size = 16.dp,
                                            index = peerIdx,
                                            avatar = peerBot?.avatar,
                                            color = peerBot?.color,
                                        )
                                        Text(
                                            text = "${store.t("collabWith", mapOf("peer" to peerName))} · ${store.t("collabCount", mapOf("count" to entry.items.size))}",
                                            color = peerPal.collabInk,
                                            fontSize = PebbleFont.secondary,
                                            fontWeight = FontWeight.SemiBold,
                                        )
                                        Icon(painterResource( PebbleIcons.ChevronRight),
                                            contentDescription = null,
                                            tint = peerPal.collabInk.copy(alpha = 0.75f),
                                            modifier = Modifier.size(16.dp),
                                        )
                                    }
                                }
                            }
                            is TimelineEntry.Message -> {
                                MessageBubble(
                                    store = store,
                                    msg = entry.msg,
                                    soft = pal.soft,
                                    onPreviewMedia = { previewMedia = it },
                                    botId = botId,
                                )
                            }
                        }
                    }
                }

                if (bot?.isBusy == true) {
                    item(key = "busy_indicator") {
                        Row(
                            modifier = Modifier
                                .widthIn(max = 840.dp)
                                .fillMaxWidth()
                                .padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            BotAvatar(
                                id = botId,
                                size = 24.dp,
                                index = index,
                                avatar = bot.avatar,
                                color = bot.color,
                                busy = true,
                            )
                            val tool = store.tools[botId].orEmpty()
                            Text(
                                text = if (tool.isNotEmpty()) store.t("callingTool", mapOf("tool" to tool)) else store.t("processing"),
                                color = p.busy,
                                fontSize = PebbleFont.body,
                            )
                        }
                    }
                }

                botQuestions.forEach { q ->
                    item(key = "q_${q.id}") {
                        Box(modifier = Modifier.widthIn(max = 840.dp)) {
                            QuestionCardView(store = store, question = q, pal = pal)
                        }
                    }
                }

                if (sendErr.isNotEmpty()) {
                    item(key = "send_err") {
                        Box(modifier = Modifier.widthIn(max = 840.dp)) {
                            StatusTextBanner(text = sendErr, error = true)
                        }
                    }
                }

                item(key = "bottom_pad") {
                    Spacer(Modifier.height(4.dp))
                }
            }

            // Floating bottom composer
            val showSlashAC = slashQuery != null && filteredCommands.isNotEmpty()
            val showMentionAC = activeMentionRange != null && mentions.isNotEmpty()
            val hasContent = input.trim().isNotEmpty() || pendingAttachments.isNotEmpty()
            val hasUploading = pendingAttachments.any { it.uploading }

            Column(
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .widthIn(max = 840.dp)
                    .fillMaxWidth()
                    .padding(horizontal = if (compact) 10.dp else 14.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (showSlashAC || showMentionAC) {
                    Surface(
                        color = p.card,
                        shape = RoundedCornerShape(16.dp),
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(max = 220.dp)
                            .shadow(10.dp, RoundedCornerShape(16.dp)),
                    ) {
                        LazyColumn(contentPadding = PaddingValues(6.dp)) {
                            if (showSlashAC) {
                                itemsIndexed(filteredCommands.take(12), key = { _, c -> c.id }) { _, cmd ->
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(RoundedCornerShape(10.dp))
                                            .clickable {
                                                val immediate = setOf("status", "bots", "reset", "compact", "clear", "cost", "context")
                                                acDismissed = true
                                                input = if (cmd.name in immediate && cmd.argumentHint.isNullOrEmpty()) {
                                                    "/${cmd.name}"
                                                } else {
                                                    "/${cmd.name} "
                                                }
                                            }
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                                    ) {
                                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                            val hint = cmd.argumentHint?.takeIf { it.isNotEmpty() }?.let { " $it" }.orEmpty()
                                            Text(
                                                text = "/${cmd.name}$hint",
                                                fontFamily = MonoFont,
                                                fontSize = PebbleFont.body,
                                                fontWeight = FontWeight.Medium,
                                                color = p.text,
                                            )
                                            if (!cmd.description.isNullOrEmpty()) {
                                                Text(cmd.description, color = p.mute, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                            }
                                        }
                                        TagPill(
                                            text = when (cmd.source) {
                                                "hub" -> store.t("cmdSourceHub")
                                                "skill" -> store.t("cmdSourceSkill")
                                                "workflow" -> store.t("cmdSourceWorkflow")
                                                else -> store.t("cmdSourceHarness")
                                            },
                                            bg = p.s1,
                                            fg = p.text2,
                                        )
                                    }
                                }
                            } else if (activeMentionRange != null) {
                                val range = activeMentionRange.first
                                itemsIndexed(mentions.take(12), key = { _, m -> m.id }) { _, item ->
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(RoundedCornerShape(10.dp))
                                            .clickable {
                                                acDismissed = true
                                                val before = input.substring(0, range.first)
                                                input = "$before@${item.value} "
                                            }
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                                    ) {
                                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                            Text(
                                                text = if (item.kind == "file") item.value else "@${item.value}",
                                                fontFamily = MonoFont,
                                                fontSize = PebbleFont.body,
                                                fontWeight = FontWeight.Medium,
                                                color = p.text,
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                            )
                                            if (!item.description.isNullOrEmpty() && item.description != item.value) {
                                                Text(item.description, color = p.mute, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                            }
                                        }
                                        TagPill(
                                            text = when (item.kind) {
                                                "bot" -> store.t("mentionBot")
                                                "agent" -> store.t("mentionAgent")
                                                else -> store.t("mentionFile")
                                            },
                                            bg = p.s1,
                                            fg = p.text2,
                                        )
                                    }
                                }
                            }
                        }
                    }
                }

                if (pendingAttachments.isNotEmpty()) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState()),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        pendingAttachments.forEach { att ->
                            Box(modifier = Modifier.padding(top = 6.dp, end = 6.dp)) {
                                val borderColor = if (att.error != null) p.danger else p.s2
                                if (att.isImage && att.previewBytes != null) {
                                    val bmp = remember(att.id) {
                                        BitmapFactory.decodeByteArray(att.previewBytes, 0, att.previewBytes.size)
                                    }
                                    if (bmp != null) {
                                        Image(
                                            bitmap = bmp.asImageBitmap(),
                                            contentDescription = att.name,
                                            contentScale = ContentScale.Crop,
                                            modifier = Modifier
                                                .size(54.dp)
                                                .clip(RoundedCornerShape(10.dp))
                                                .border(1.dp, borderColor, RoundedCornerShape(10.dp)),
                                        )
                                    }
                                } else {
                                    Row(
                                        modifier = Modifier
                                            .widthIn(max = 180.dp)
                                            .clip(RoundedCornerShape(12.dp))
                                            .background(p.card)
                                            .border(1.dp, borderColor, RoundedCornerShape(12.dp))
                                            .padding(horizontal = 10.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                                    ) {
                                        Icon(painterResource(PebbleIcons.Description), contentDescription = null, tint = p.text2, modifier = Modifier.size(15.dp))
                                        Text(att.name, color = p.text, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                                        if (att.uploading) {
                                            CircularProgressIndicator(modifier = Modifier.size(12.dp), strokeWidth = 1.5.dp, color = p.accent)
                                        }
                                    }
                                }
                                Box(
                                    modifier = Modifier
                                        .align(Alignment.TopEnd)
                                        .size(18.dp)
                                        .clip(CircleShape)
                                        .background(p.text)
                                        .clickable { pendingAttachments.removeAll { it.id == att.id } },
                                    contentAlignment = Alignment.Center,
                                ) {
                                    Icon(painterResource(PebbleIcons.Close), contentDescription = null, tint = p.card, modifier = Modifier.size(12.dp))
                                }
                            }
                        }
                    }
                }

                // Floating Input Row
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.Bottom,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Box {
                        Surface(
                            color = p.card,
                            shape = CircleShape,
                            shadowElevation = 3.dp,
                            modifier = Modifier
                                .size(44.dp)
                                .clip(CircleShape)
                                .clickable { attachMenuOpen = true },
                        ) {
                            Box(contentAlignment = Alignment.Center) {
                                Icon(painterResource(PebbleIcons.Add), contentDescription = store.t("attachFile"), tint = p.text, modifier = Modifier.size(22.dp))
                            }
                        }
                        DropdownMenu(
                            expanded = attachMenuOpen,
                            onDismissRequest = { attachMenuOpen = false },
                            containerColor = p.card,
                        ) {
                            DropdownMenuItem(
                                text = { Text(store.t("photoLibrary"), color = p.text) },
                                leadingIcon = { Icon(painterResource(PebbleIcons.PhotoLibrary), contentDescription = null, tint = p.text2) },
                                onClick = {
                                    attachMenuOpen = false
                                    photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                                },
                            )
                            DropdownMenuItem(
                                text = { Text(store.t("chooseFile"), color = p.text) },
                                leadingIcon = { Icon(painterResource(PebbleIcons.Description), contentDescription = null, tint = p.text2) },
                                onClick = {
                                    attachMenuOpen = false
                                    filePicker.launch(arrayOf("*/*"))
                                },
                            )
                        }
                    }

                    Surface(
                        color = p.card,
                        shape = PebbleDimens.composerShape,
                        shadowElevation = 3.dp,
                        modifier = Modifier
                            .weight(1f)
                            .border(0.5.dp, p.s2, PebbleDimens.composerShape),
                    ) {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 16.dp, vertical = 11.dp),
                            contentAlignment = Alignment.CenterStart,
                        ) {
                            if (input.isEmpty()) {
                                Text(
                                    text = store.t("msgBotPh", mapOf("name" to botName)),
                                    color = p.mute,
                                    fontSize = PebbleFont.body,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                            BasicTextField(
                                value = input,
                                onValueChange = { onInputChange(it) },
                                textStyle = TextStyle(color = p.text, fontSize = PebbleFont.body, lineHeight = 22.sp),
                                cursorBrush = SolidColor(pal.accent),
                                maxLines = 6,
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                    }

                    val sendEnabled = showStop || (hasContent && !hasUploading && !sending)
                    val btnBg = when {
                        showStop -> p.danger
                        sendEnabled -> pal.accent
                        else -> p.s2
                    }
                    val btnFg = when {
                        showStop -> Color.White
                        sendEnabled -> pal.onAccent
                        else -> p.mute
                    }
                    Surface(
                        color = btnBg,
                        shape = CircleShape,
                        shadowElevation = if (sendEnabled) 3.dp else 0.dp,
                        modifier = Modifier
                            .size(44.dp)
                            .clip(CircleShape)
                            .clickable(enabled = sendEnabled) {
                                if (showStop) interrupt() else submit()
                            },
                    ) {
                        Box(contentAlignment = Alignment.Center) {
                            Icon(
                                painter = painterResource(if (showStop) PebbleIcons.Stop else PebbleIcons.ArrowUpward),
                                contentDescription = store.t(if (showStop) "stopOutput" else "sendMsg"),
                                tint = btnFg,
                                modifier = Modifier.size(if (showStop) 18.dp else 22.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}

private fun shouldShowStamp(cur: String?, prev: String?): Boolean {
    if (prev == null) return true
    val a = parseIsoInstant(cur) ?: return false
    val b = parseIsoInstant(prev) ?: return false
    return Duration.between(b, a).seconds > 300
}

private data class PickedPayload(val name: String, val mime: String, val isImage: Boolean, val bytes: ByteArray)

private fun readUriPayload(context: Context, uri: Uri, forceImage: Boolean): PickedPayload? {
    val resolver = context.contentResolver
    val mime = resolver.getType(uri) ?: if (forceImage) "image/jpeg" else "application/octet-stream"
    var name = ""
    runCatching {
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (idx >= 0) name = c.getString(idx).orEmpty()
            }
        }
    }
    if (name.isBlank()) {
        name = if (forceImage) "photo_${System.currentTimeMillis()}.jpg" else "file_${System.currentTimeMillis()}"
    }
    val bytes = resolver.openInputStream(uri)?.use { it.readBytes() } ?: return null
    val isImg = forceImage || mime.startsWith("image/")
    return PickedPayload(name = name, mime = mime, isImage = isImg, bytes = bytes)
}
