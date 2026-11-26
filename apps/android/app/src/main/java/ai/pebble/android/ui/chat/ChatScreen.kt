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
import androidx.compose.material3.HorizontalDivider
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
    var inspectTaskId by remember(botId) { mutableStateOf<String?>(null) }
    var previewMedia by remember(botId) { mutableStateOf<MessageMedia?>(null) }
    var commands by remember(botId) { mutableStateOf<List<SlashCommandItem>>(emptyList()) }
    var mentions by remember(botId) { mutableStateOf<List<MentionCandidate>>(emptyList()) }
    var mentionJob by remember(botId) { mutableStateOf<Job?>(null) }
    var acDismissed by remember(botId) { mutableStateOf(false) }
    val pendingAttachments = remember(botId) { mutableStateListOf<PendingAttachment>() }

    val index = store.bots.indexOfFirst { it.id == botId }
    val bot = if (index >= 0) store.bots[index] else null
    val botName = bot?.displayName ?: botId
    val pal = p.bot(botId, index, bot?.color)
    val messages = store.msgs[botId].orEmpty()
    val botApprovals = store.approvals.filter { it.botId == botId }
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
        runCatching { store.fetchBotTasks(botId) }
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

    LaunchedEffect(timelineEntries.size, botApprovals.size, botQuestions.size, bot?.isBusy) {
        if (timelineEntries.isNotEmpty() || botApprovals.isNotEmpty() || botQuestions.isNotEmpty()) {
            val total = timelineEntries.size + botApprovals.size + botQuestions.size + 1
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

    inspectTaskId?.let { tid ->
        ModalBottomSheet(
            onDismissRequest = { inspectTaskId = null },
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
            containerColor = p.page,
        ) {
            Box(modifier = Modifier.heightIn(min = 380.dp, max = 720.dp)) {
                TaskInspectorSheet(
                    store = store,
                    botId = botId,
                    taskId = tid,
                    onDismiss = { inspectTaskId = null },
                    onOpenDesktop = onOpenDesktop,
                )
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(if (inDesktopSidebar) p.card else p.page),
    ) {
        ChatTopBar(
            store = store,
            botId = botId,
            bot = bot,
            index = index,
            botName = botName,
            compact = compact,
            inDesktopSidebar = inDesktopSidebar,
            onBack = onBack,
            onOpenDesktop = onOpenDesktop,
            onOpenFiles = onOpenFiles,
            onToggleDetail = onToggleDetail,
            onOpenBotInfo = onOpenBotInfo,
            onSelectBot = onSelectBot,
            onCloseSidebar = onCloseSidebar,
        )

        ChatStatusBanners(
            store = store,
            pal = pal,
            inDesktopSidebar = inDesktopSidebar,
            onOpenDesktop = onOpenDesktop,
        )

        if (!inDesktopSidebar) {
            BotChatTaskStrip(
                store = store,
                botId = botId,
                pal = pal,
                onSelectTask = { inspectTaskId = it.id },
            )
        }

        // Message List + Floating Composer overlaid cleanly without black bottom cutoff
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars)),
        ) {
            ChatMessageList(
                store = store,
                botId = botId,
                bot = bot,
                index = index,
                botName = botName,
                pal = pal,
                inDesktopSidebar = inDesktopSidebar,
                compact = compact,
                listState = listState,
                timelineEntries = timelineEntries,
                botApprovals = botApprovals,
                botQuestions = botQuestions,
                sendErr = sendErr,
                isEmptyMessages = messages.isEmpty(),
                onCollabClick = { if (it.isNotEmpty()) collabPeerId = it },
                onPreviewMedia = { previewMedia = it },
            )

            ChatComposer(
                store = store,
                botId = botId,
                bot = bot,
                botName = botName,
                pal = pal,
                compact = compact,
                inDesktopSidebar = inDesktopSidebar,
                input = input,
                sending = sending,
                showStop = showStop,
                slashQuery = slashQuery,
                filteredCommands = filteredCommands,
                activeMentionRange = activeMentionRange,
                mentions = mentions,
                pendingAttachments = pendingAttachments,
                onInputChange = ::onInputChange,
                onUpdateInput = { input = it },
                onDismissAutocomplete = { acDismissed = true },
                onRemoveAttachment = { id -> pendingAttachments.removeAll { it.id == id } },
                onPickPhotos = { photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
                onPickFiles = { filePicker.launch(arrayOf("*/*")) },
                onInterrupt = ::interrupt,
                onSubmit = ::submit,
                onError = { sendErr = it },
            )
        }
    }
}
