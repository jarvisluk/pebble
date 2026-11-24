package ai.pebble.android.ui.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.ChatMessage
import ai.pebble.android.data.MessageMedia
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.LocalPalette

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CollabSheetView(
    store: PebbleStore,
    botId: String,
    peerId: String,
    onClose: () -> Unit,
) {
    val p = LocalPalette.current
    var history by remember { mutableStateOf<List<ChatMessage>>(emptyList()) }
    var loading by remember { mutableStateOf(false) }
    var previewMedia by remember { mutableStateOf<MessageMedia?>(null) }

    val botIdx = store.bots.indexOfFirst { it.id == botId }.coerceAtLeast(0)
    val peerIdx = store.bots.indexOfFirst { it.id == peerId }.coerceAtLeast(0)
    val botObj = store.bots.firstOrNull { it.id == botId }
    val peerObj = store.bots.firstOrNull { it.id == peerId }
    val botName = botObj?.displayName ?: botId
    val peerName = peerObj?.displayName ?: peerId
    val botPal = p.bot(botId, botIdx, botObj?.color)
    val peerPal = p.bot(peerId, peerIdx, peerObj?.color)

    LaunchedEffect(botId, peerId) {
        loading = true
        history = runCatching { store.loadCollabMessages(botId, peerId) }.getOrDefault(emptyList())
        loading = false
    }

    val items = remember(history, store.msgs[botId], botId, peerId) {
        val peerTag = "bot:$peerId"
        val map = linkedMapOf<String, ChatMessage>()
        history.forEach { map[it.id] = it }
        store.msgs[botId].orEmpty()
            .filter { (it.role == "bot_out" || it.role == "bot_in") && (it.from == peerTag || it.to == peerTag) }
            .forEach { map[it.id] = it }
        map.values.sortedBy { it.createdAt.orEmpty() }
    }

    previewMedia?.let { media ->
        MediaPreviewDialog(store = store, media = media, onDismiss = { previewMedia = null })
    }

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
                        Icon(Icons.Rounded.Close, contentDescription = store.t("close"), tint = p.text)
                    }
                },
                title = {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        BotAvatar(
                            id = peerId,
                            size = 22.dp,
                            index = peerIdx,
                            avatar = peerObj?.avatar,
                            color = peerObj?.color,
                        )
                        Text(
                            text = store.t("collabTitle", mapOf("bot" to botName, "peer" to peerName)),
                            fontSize = PebbleFont.title,
                            fontWeight = FontWeight.Bold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
            )
        },
    ) { innerPadding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            when {
                loading && items.isEmpty() -> {
                    CircularProgressIndicator(
                        color = p.accent,
                        modifier = Modifier.align(Alignment.Center),
                    )
                }
                items.isEmpty() -> {
                    Text(
                        text = store.t("collabEmpty"),
                        color = p.mute,
                        fontSize = PebbleFont.body,
                        modifier = Modifier.align(Alignment.Center),
                    )
                }
                else -> {
                    LazyColumn(
                        modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(16.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(items, key = { it.id }) { m ->
                            val isOut = m.role == "bot_out"
                            val pal = if (isOut) botPal else peerPal
                            val senderId = if (isOut) botId else peerId
                            val senderIdx = if (isOut) botIdx else peerIdx
                            val senderObj = if (isOut) botObj else peerObj
                            Column(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalAlignment = if (isOut) Alignment.End else Alignment.Start,
                                verticalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    if (!isOut) {
                                        BotAvatar(
                                            id = senderId,
                                            size = 18.dp,
                                            index = senderIdx,
                                            avatar = senderObj?.avatar,
                                            color = senderObj?.color,
                                        )
                                    }
                                    Text(
                                        text = if (isOut) store.t("delegateTo", mapOf("peer" to peerName))
                                        else store.t("fromPeer", mapOf("peer" to peerName)),
                                        color = pal.collabInk,
                                        fontSize = PebbleFont.caption,
                                        fontWeight = FontWeight.SemiBold,
                                    )
                                    if (isOut) {
                                        BotAvatar(
                                            id = senderId,
                                            size = 18.dp,
                                            index = senderIdx,
                                            avatar = senderObj?.avatar,
                                            color = senderObj?.color,
                                        )
                                    }
                                }
                                MessageBubble(
                                    store = store,
                                    msg = m,
                                    soft = p.s1,
                                    onPreviewMedia = { previewMedia = it },
                                    showCollabHeader = false,
                                    bubbleColor = pal.collabBg,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}
