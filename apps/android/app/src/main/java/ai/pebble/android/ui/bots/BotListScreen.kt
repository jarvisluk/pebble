package ai.pebble.android.ui.bots

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Add
import androidx.compose.material.icons.rounded.ArrowDownward
import androidx.compose.material.icons.rounded.ArrowUpward
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.DeleteOutline
import androidx.compose.material.icons.rounded.MarkEmailRead
import androidx.compose.material.icons.rounded.MarkEmailUnread
import androidx.compose.material.icons.rounded.PanTool
import androidx.compose.material.icons.rounded.PushPin
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.Settings
import androidx.compose.material.icons.rounded.SwapVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.BotSortMode
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.formatShortTime
import ai.pebble.android.data.plainPreview
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BotListScreen(
    store: PebbleStore,
    selectedBotId: String?,
    compact: Boolean,
    onSelectBot: (String) -> Unit,
    onDeletedBot: (String) -> Unit,
    onOpenSettings: () -> Unit,
    onOpenDesktop: (String) -> Unit,
    onCreateBot: (() -> Unit)? = null,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var query by remember { mutableStateOf("") }
    var refreshing by remember { mutableStateOf(false) }
    var sortMenuOpen by remember { mutableStateOf(false) }
    var confirm by remember { mutableStateOf<ConfirmItem?>(null) }

    val orderedBots = remember(
        store.bots,
        query,
        store.pinnedIds,
        store.sortMode,
        store.manualOrder,
        store.last.toMap(),
    ) {
        val q = query.trim().lowercase()
        val indexed = store.bots.mapIndexed { idx, bot -> idx to bot }.filter { (_, b) ->
            q.isEmpty() || b.displayName.lowercase().contains(q) || b.id.lowercase().contains(q) ||
                b.description.orEmpty().lowercase().contains(q)
        }
        indexed.sortedWith { a, b ->
            val aPinned = store.pinnedIds.indexOf(a.second.id).takeIf { it >= 0 }
            val bPinned = store.pinnedIds.indexOf(b.second.id).takeIf { it >= 0 }
            if (aPinned != null && bPinned != null) return@sortedWith aPinned.compareTo(bPinned)
            if (aPinned != null) return@sortedWith -1
            if (bPinned != null) return@sortedWith 1
            when (store.sortMode) {
                BotSortMode.RECENT -> {
                    val ta = store.last[a.second.id]?.createdAt.orEmpty()
                    val tb = store.last[b.second.id]?.createdAt.orEmpty()
                    if (ta != tb) return@sortedWith tb.compareTo(ta)
                    a.first.compareTo(b.first)
                }
                BotSortMode.MANUAL -> {
                    val ma = store.manualOrder.indexOf(a.second.id).takeIf { it >= 0 }
                    val mb = store.manualOrder.indexOf(b.second.id).takeIf { it >= 0 }
                    if (ma != null && mb != null) return@sortedWith ma.compareTo(mb)
                    if (ma != null) return@sortedWith -1
                    if (mb != null) return@sortedWith 1
                    a.first.compareTo(b.first)
                }
            }
        }
    }

    fun moveBot(botId: String, delta: Int) {
        val ids = orderedBots.map { it.second.id }.toMutableList()
        val idx = ids.indexOf(botId)
        val target = idx + delta
        if (idx < 0 || target !in ids.indices) return
        ids.removeAt(idx)
        ids.add(target, botId)
        store.moveManualOrder(ids)
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(if (compact) p.page else p.card),
    ) {
        if (compact) {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.page,
                    titleContentColor = p.text,
                ),
                navigationIcon = {
                    Box {
                        IconButton(onClick = { sortMenuOpen = true }) {
                            Icon(Icons.Rounded.SwapVert, contentDescription = store.t("sortBots"), tint = p.text)
                        }
                        DropdownMenu(
                            expanded = sortMenuOpen,
                            onDismissRequest = { sortMenuOpen = false },
                            containerColor = p.card,
                        ) {
                            DropdownMenuItem(
                                text = { Text(store.t("sortByRecent"), color = p.text) },
                                trailingIcon = {
                                    if (store.sortMode == BotSortMode.RECENT) {
                                        Icon(Icons.Rounded.Check, contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                    }
                                },
                                onClick = {
                                    store.updateSortMode(BotSortMode.RECENT)
                                    sortMenuOpen = false
                                },
                            )
                            DropdownMenuItem(
                                text = { Text(store.t("sortManual"), color = p.text) },
                                trailingIcon = {
                                    if (store.sortMode == BotSortMode.MANUAL) {
                                        Icon(Icons.Rounded.Check, contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                    }
                                },
                                onClick = {
                                    store.updateSortMode(BotSortMode.MANUAL)
                                    sortMenuOpen = false
                                },
                            )
                        }
                    }
                },
                title = {},
                actions = {
                    IconButton(onClick = onOpenSettings) {
                        Icon(Icons.Rounded.Settings, contentDescription = store.t("settings"), tint = p.text)
                    }
                },
            )
        } else {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Surface(
                    color = p.s1,
                    shape = RoundedCornerShape(10.dp),
                    modifier = Modifier
                        .weight(1f)
                        .height(36.dp),
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxSize()
                            .padding(horizontal = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(Icons.Rounded.Search, contentDescription = null, tint = p.mute, modifier = Modifier.size(17.dp))
                        Box(modifier = Modifier.weight(1f)) {
                            if (query.isEmpty()) {
                                Text(store.t("searchBot"), color = p.mute, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                            BasicTextField(
                                value = query,
                                onValueChange = { query = it },
                                singleLine = true,
                                textStyle = TextStyle(color = p.text, fontSize = PebbleFont.secondary),
                                cursorBrush = SolidColor(p.accent),
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                        if (query.isNotEmpty()) {
                            Icon(
                                imageVector = Icons.Rounded.Close,
                                contentDescription = store.t("clear"),
                                tint = p.mute,
                                modifier = Modifier
                                    .size(16.dp)
                                    .clickable { query = "" },
                            )
                        }
                    }
                }
                Box {
                    IconButton(
                        onClick = { sortMenuOpen = true },
                        modifier = Modifier.size(34.dp),
                    ) {
                        Icon(Icons.Rounded.SwapVert, contentDescription = store.t("sortBots"), tint = p.text2, modifier = Modifier.size(19.dp))
                    }
                    DropdownMenu(
                        expanded = sortMenuOpen,
                        onDismissRequest = { sortMenuOpen = false },
                        containerColor = p.card,
                    ) {
                        DropdownMenuItem(
                            text = { Text(store.t("sortByRecent"), color = p.text) },
                            trailingIcon = {
                                if (store.sortMode == BotSortMode.RECENT) {
                                    Icon(Icons.Rounded.Check, contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                }
                            },
                            onClick = {
                                store.updateSortMode(BotSortMode.RECENT)
                                sortMenuOpen = false
                            },
                        )
                        DropdownMenuItem(
                            text = { Text(store.t("sortManual"), color = p.text) },
                            trailingIcon = {
                                if (store.sortMode == BotSortMode.MANUAL) {
                                    Icon(Icons.Rounded.Check, contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                }
                            },
                            onClick = {
                                store.updateSortMode(BotSortMode.MANUAL)
                                sortMenuOpen = false
                            },
                        )
                    }
                }
            }
        }

        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                refreshing = true
                scope.launch {
                    runCatching {
                        if (store.isConnected) store.refreshBots() else store.reconnect()
                    }
                    refreshing = false
                }
            },
            modifier = Modifier.weight(1f).fillMaxWidth(),
        ) {
            LazyColumn(
                modifier = Modifier
                    .fillMaxSize()
                    .let { if (compact) it.navigationBarsPadding() else it },
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (compact) {
                    // Search bar
                    item(key = "search") {
                        Surface(
                            color = p.card,
                            shape = RoundedCornerShape(12.dp),
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(40.dp),
                        ) {
                            Row(
                                modifier = Modifier
                                    .fillMaxSize()
                                    .padding(horizontal = 12.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Icon(Icons.Rounded.Search, contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                                Box(modifier = Modifier.weight(1f)) {
                                    if (query.isEmpty()) {
                                        Text(store.t("searchBot"), color = p.mute, fontSize = PebbleFont.body)
                                    }
                                    BasicTextField(
                                        value = query,
                                        onValueChange = { query = it },
                                        singleLine = true,
                                        textStyle = TextStyle(color = p.text, fontSize = PebbleFont.body),
                                        cursorBrush = SolidColor(p.accent),
                                        modifier = Modifier.fillMaxWidth(),
                                    )
                                }
                                if (query.isNotEmpty()) {
                                    Icon(
                                        imageVector = Icons.Rounded.Close,
                                        contentDescription = store.t("clear"),
                                        tint = p.mute,
                                        modifier = Modifier
                                            .size(18.dp)
                                            .clickable { query = "" },
                                    )
                                }
                            }
                        }
                    }
                }

                // Active takeover alert banner
                val to = store.takeovers.firstOrNull()
                if (to != null) {
                    item(key = "takeover_${to.id}") {
                        Surface(
                            color = p.pink,
                            shape = RoundedCornerShape(16.dp),
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(RoundedCornerShape(16.dp))
                                .clickable { onOpenDesktop(to.target ?: "local") },
                        ) {
                            Row(
                                modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Icon(Icons.Rounded.PanTool, contentDescription = null, tint = p.pinkInk, modifier = Modifier.size(18.dp))
                                Text(
                                    text = store.t(
                                        "takeoverBanner",
                                        mapOf("bot" to (to.botName ?: to.botId ?: "Bot"), "reason" to to.reason.orEmpty()),
                                    ),
                                    color = p.pinkInk,
                                    fontSize = PebbleFont.secondary,
                                    fontWeight = FontWeight.Medium,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f),
                                )
                                TextButton(
                                    onClick = { onOpenDesktop(to.target ?: "local") },
                                    contentPadding = PaddingValues(horizontal = 8.dp),
                                ) {
                                    Text(store.t("handleAction"), color = p.pinkInk, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                                }
                            }
                        }
                    }
                }

                // Bots card
                if (orderedBots.isEmpty()) {
                    item(key = "empty") {
                        Text(
                            text = if (query.isEmpty()) store.t("noBotsYet") else store.t("noMatchedBots"),
                            color = p.mute,
                            fontSize = PebbleFont.body,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 20.dp),
                        )
                    }
                } else {
                    item(key = "bots_card") {
                        Surface(
                            color = p.card,
                            shape = PebbleDimens.cardShape,
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Column(modifier = Modifier.fillMaxWidth()) {
                                orderedBots.forEachIndexed { pos, (rawIdx, bot) ->
                                    if (pos > 0) {
                                        HorizontalDivider(
                                            modifier = Modifier.padding(start = 64.dp, end = 14.dp),
                                            thickness = 0.5.dp,
                                            color = p.s2,
                                        )
                                    }
                                    val pinned = bot.id in store.pinnedIds
                                    val isUnread = (store.unread[bot.id] ?: 0) > 0
                                    val showMove = store.sortMode == BotSortMode.MANUAL && query.isEmpty() && !pinned && orderedBots.size > 1
                                    BotListRow(
                                        store = store,
                                        bot = bot,
                                        index = rawIdx,
                                        selected = selectedBotId == bot.id,
                                        pinned = pinned,
                                        isUnread = isUnread,
                                        showMoveControls = showMove,
                                        canMoveUp = pos > 0 && orderedBots[pos - 1].second.id !in store.pinnedIds,
                                        canMoveDown = pos < orderedBots.lastIndex,
                                        onMoveUp = { moveBot(bot.id, -1) },
                                        onMoveDown = { moveBot(bot.id, 1) },
                                        onClick = { onSelectBot(bot.id) },
                                        onTogglePin = { store.togglePin(bot.id) },
                                        onToggleUnread = { store.toggleUnread(bot.id) },
                                        onDelete = if (store.bots.size > 1) {
                                            {
                                                confirm = ConfirmItem(
                                                    title = store.t("deleteBotTitle", mapOf("name" to bot.displayName)),
                                                    message = store.t("deleteBotDesc"),
                                                ) {
                                                    scope.launch {
                                                        runCatching {
                                                            store.deleteBot(bot.id)
                                                            onDeletedBot(bot.id)
                                                        }
                                                    }
                                                }
                                            }
                                        } else null,
                                    )
                                }
                            }
                        }
                    }
                }

                item(key = "bottom_spacer") {
                    Spacer(Modifier.height(12.dp))
                }
            }
        }
        if (!compact) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .navigationBarsPadding()
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onCreateBot?.invoke() }
                        .padding(horizontal = 6.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    Icon(Icons.Rounded.Add, contentDescription = null, tint = p.text, modifier = Modifier.size(17.dp))
                    Text(store.t("newBot"), color = p.text, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                }
                Spacer(Modifier.weight(1f))
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onOpenSettings() }
                        .padding(horizontal = 6.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    Icon(Icons.Rounded.Settings, contentDescription = null, tint = p.text2, modifier = Modifier.size(17.dp))
                    Text(store.t("settings"), color = p.text2, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun BotListRow(
    store: PebbleStore,
    bot: BotStatus,
    index: Int,
    selected: Boolean,
    pinned: Boolean,
    isUnread: Boolean,
    showMoveControls: Boolean,
    canMoveUp: Boolean,
    canMoveDown: Boolean,
    onMoveUp: () -> Unit,
    onMoveDown: () -> Unit,
    onClick: () -> Unit,
    onTogglePin: () -> Unit,
    onToggleUnread: () -> Unit,
    onDelete: (() -> Unit)?,
) {
    val p = LocalPalette.current
    var menuOpen by remember { mutableStateOf(false) }

    val lastMsg = store.last[bot.id]
    val unreadCount = store.unread[bot.id] ?: 0
    val tool = store.tools[bot.id].orEmpty()
    val preview = when {
        bot.isBusy -> if (tool.isNotEmpty()) store.t("callingTool", mapOf("tool" to tool)) else store.t("thinking")
        lastMsg != null -> {
            val prefix = if (lastMsg.role == "user") store.t("youPrefix") else ""
            val txt = plainPreview(lastMsg.body)
            val display = if (txt.isEmpty() && !lastMsg.media.isNullOrEmpty()) "[${store.t("attachments")}]" else txt
            "$prefix$display"
        }
        else -> bot.description.orEmpty()
    }

    Box {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(if (selected) p.sel else p.card)
                .combinedClickable(
                    onClick = onClick,
                    onLongClick = { menuOpen = true },
                )
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            BotAvatar(
                id = bot.id,
                size = 40.dp,
                index = index,
                avatar = bot.avatar,
                color = bot.color,
                busy = bot.isBusy,
            )

            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Row(
                        modifier = Modifier.weight(1f),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Text(
                            text = bot.displayName,
                            color = p.text,
                            fontSize = PebbleFont.body,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f, fill = false),
                        )
                        if (pinned) {
                            Icon(
                                imageVector = Icons.Rounded.PushPin,
                                contentDescription = null,
                                tint = p.mute,
                                modifier = Modifier.size(13.dp),
                            )
                        }
                    }
                    Text(
                        text = formatShortTime(lastMsg?.createdAt),
                        color = p.mute,
                        fontSize = PebbleFont.caption,
                        maxLines = 1,
                    )
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text(
                        text = preview,
                        color = if (bot.isBusy) p.busy else p.text2,
                        fontSize = PebbleFont.secondary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    if (unreadCount > 0) {
                        Box(
                            modifier = Modifier
                                .clip(CircleShape)
                                .background(p.lilac)
                                .padding(horizontal = 7.dp, vertical = 2.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                text = "$unreadCount",
                                color = p.lilacInk,
                                fontSize = PebbleFont.caption,
                                fontWeight = FontWeight.Bold,
                            )
                        }
                    }
                }
            }

            if (showMoveControls) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    IconButton(
                        onClick = onMoveUp,
                        enabled = canMoveUp,
                        modifier = Modifier.size(30.dp),
                    ) {
                        Icon(
                            Icons.Rounded.ArrowUpward,
                            contentDescription = store.t("moveUp"),
                            tint = if (canMoveUp) p.text2 else p.mute.copy(alpha = 0.35f),
                            modifier = Modifier.size(16.dp),
                        )
                    }
                    IconButton(
                        onClick = onMoveDown,
                        enabled = canMoveDown,
                        modifier = Modifier.size(30.dp),
                    ) {
                        Icon(
                            Icons.Rounded.ArrowDownward,
                            contentDescription = store.t("moveDown"),
                            tint = if (canMoveDown) p.text2 else p.mute.copy(alpha = 0.35f),
                            modifier = Modifier.size(16.dp),
                        )
                    }
                }
            }
        }

        DropdownMenu(
            expanded = menuOpen,
            onDismissRequest = { menuOpen = false },
            containerColor = p.card,
        ) {
            DropdownMenuItem(
                text = { Text(store.t(if (pinned) "unpinBot" else "pinBot"), color = p.text) },
                leadingIcon = { Icon(Icons.Rounded.PushPin, contentDescription = null, tint = p.text2, modifier = Modifier.size(18.dp)) },
                onClick = {
                    menuOpen = false
                    onTogglePin()
                },
            )
            DropdownMenuItem(
                text = { Text(store.t(if (isUnread) "markRead" else "markUnread"), color = p.text) },
                leadingIcon = {
                    Icon(
                        if (isUnread) Icons.Rounded.MarkEmailRead else Icons.Rounded.MarkEmailUnread,
                        contentDescription = null,
                        tint = p.text2,
                        modifier = Modifier.size(18.dp),
                    )
                },
                onClick = {
                    menuOpen = false
                    onToggleUnread()
                },
            )
            if (onDelete != null) {
                HorizontalDivider(color = p.s2)
                DropdownMenuItem(
                    text = { Text(store.t("delete"), color = p.danger) },
                    leadingIcon = { Icon(Icons.Rounded.DeleteOutline, contentDescription = null, tint = p.danger, modifier = Modifier.size(18.dp)) },
                    onClick = {
                        menuOpen = false
                        onDelete()
                    },
                )
            }
        }
    }
}
