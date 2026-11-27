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
import ai.pebble.android.ui.theme.BotAccent
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


internal sealed interface TimelineEntry {
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

@Composable
internal fun ChatMessageList(
    store: PebbleStore,
    botId: String,
    bot: ai.pebble.android.data.BotStatus?,
    index: Int,
    botName: String,
    pal: ai.pebble.android.ui.theme.BotAccent,
    inDesktopSidebar: Boolean,
    compact: Boolean,
    listState: androidx.compose.foundation.lazy.LazyListState,
    timelineEntries: List<TimelineEntry>,
    botApprovals: List<ai.pebble.android.data.PendingApproval>,
    botQuestions: List<ai.pebble.android.data.PendingQuestion>,
    sendErr: String,
    isEmptyMessages: Boolean,
    onCollabClick: (String) -> Unit,
    onPreviewMedia: (MessageMedia) -> Unit,
    onOpenStageRef: (ai.pebble.android.data.StageRef) -> Unit = {},
) {
    val p = LocalPalette.current
    val messagesIsEmpty = isEmptyMessages

    LazyColumn(
        state = listState,
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(
            start = if (inDesktopSidebar) 12.dp else if (compact) 10.dp else 16.dp,
            end = if (inDesktopSidebar) 12.dp else if (compact) 10.dp else 16.dp,
            top = 12.dp,
            bottom = 86.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (messagesIsEmpty) {
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
                                    if (entry.peerId.isNotEmpty()) onCollabClick(entry.peerId)
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
                            onPreviewMedia = onPreviewMedia,
                            botId = botId,
                            inSidebar = inDesktopSidebar,
                            onOpenStageRef = onOpenStageRef,
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

        botApprovals.forEach { apr ->
            item(key = "apr_${apr.id}") {
                Box(modifier = Modifier.widthIn(max = 840.dp)) {
                    ApprovalCardView(store = store, approval = apr, pal = pal)
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
}

private fun shouldShowStamp(cur: String?, prev: String?): Boolean {
    if (prev == null) return true
    val a = parseIsoInstant(cur) ?: return false
    val b = parseIsoInstant(prev) ?: return false
    return Duration.between(b, a).seconds > 300
}
