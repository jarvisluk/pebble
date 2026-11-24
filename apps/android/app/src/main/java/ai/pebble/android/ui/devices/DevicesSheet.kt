package ai.pebble.android.ui.devices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DevicesSheet(
    store: PebbleStore,
    onClose: () -> Unit,
    onManageConnections: () -> Unit,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    var refreshing by remember { mutableStateOf(false) }

    suspend fun load() {
        store.refreshMobileTailscale(includePeers = false)
        if (!store.isConnected) return
        coroutineScope {
            val a = async { runCatching { store.refreshNodes() } }
            val b = async { runCatching { store.refreshTailcat() } }
            val c = async { runCatching { store.refreshHostTailscale() } }
            a.await(); b.await(); c.await()
        }
    }

    LaunchedEffect(Unit) { load() }

    val tailcatText = when (val t = store.tailcat) {
        null -> store.t("tailcatUnknown")
        else -> when (t.state) {
            "ready" -> store.t("tailcatReady")
            "connecting" -> store.t("tailcatConnecting")
            "disabled" -> store.t("tailcatDisabled")
            else -> t.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")
        }
    }

    val hostTsText = when (val t = store.hostTailscale) {
        null -> store.t("tailcatUnknown")
        else -> when (t.state) {
            "running" -> t.tailnet ?: store.t("tailcatReady")
            "starting" -> store.t("tsBinding")
            "needs_login" -> store.t("tsNeedsLogin")
            "disabled" -> store.t("tailcatDisabled")
            else -> t.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")
        }
    }

    val mobileTsText = when (val t = store.mobileTailscale) {
        null -> store.t("tsNotBound")
        else -> when (t.state) {
            "running" -> t.tailnet ?: store.t("tailcatReady")
            "starting" -> store.t("tsBinding")
            "needs_login" -> store.t("tsNeedsLogin")
            "disabled" -> store.t("tsNotBound")
            else -> t.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")
        }
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
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                    }
                },
                title = {
                    Text(store.t("devicesTunnel"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
            )
        },
    ) { innerPadding ->
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                refreshing = true
                scope.launch {
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
                Column(
                    modifier = Modifier
                        .widthIn(max = 580.dp)
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    SectionHeader(store.t("currentMobileConn"))
                    SectionCard {
                        Column(
                            modifier = Modifier.padding(14.dp),
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            val active = store.activeServer
                            if (active != null) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Text(
                                        text = active.label,
                                        color = p.text,
                                        fontSize = PebbleFont.body,
                                        fontWeight = FontWeight.Medium,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                        modifier = Modifier.weight(1f),
                                    )
                                    Text(
                                        text = active.address,
                                        color = p.text2,
                                        fontSize = PebbleFont.secondary,
                                        fontFamily = MonoFont,
                                        maxLines = 1,
                                        overflow = TextOverflow.MiddleEllipsis,
                                        textAlign = TextAlign.End,
                                        modifier = Modifier.weight(1f),
                                    )
                                }
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    TagPill(
                                        text = when {
                                            store.isTailscale -> store.t("tailscaleE2E")
                                            store.isTailcat -> store.t("tailcatE2E")
                                            else -> store.t("httpLan")
                                        },
                                        bg = p.lilac,
                                        fg = p.lilacInk,
                                    )
                                    TagPill(
                                        text = if (store.online) store.t("streamConnected") else store.t("reconnecting"),
                                        bg = if (store.online) p.sage else p.s2,
                                        fg = if (store.online) p.sageInk else p.text2,
                                    )
                                }
                            } else {
                                Text(store.t("notConnectedHost"), color = p.mute, fontSize = PebbleFont.body)
                            }
                            CardDivider()
                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                Text(store.t("tsMobileStatus"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(
                                    text = mobileTsText,
                                    color = if (store.mobileTailscale?.state == "running") p.sageInk else p.text2,
                                    fontSize = PebbleFont.body,
                                )
                            }
                            CardDivider()
                            Text(
                                text = store.t("manageConn"),
                                color = p.accent,
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.Medium,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable(onClick = onManageConnections)
                                    .padding(vertical = 4.dp),
                            )
                            if (active != null) {
                                CardDivider()
                                Text(
                                    text = store.t("openHostDesktop"),
                                    color = p.accent,
                                    fontSize = PebbleFont.body,
                                    fontWeight = FontWeight.Medium,
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable { onOpenDesktop("local") }
                                        .padding(vertical = 4.dp),
                                )
                            }
                        }
                    }

                    SectionHeader(store.t("hostRemoteAccess"))
                    SectionCard {
                        Column(
                            modifier = Modifier.padding(14.dp),
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                Text(store.t("tunnelState"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                val tcColor = when (store.tailcat?.state) {
                                    "ready" -> p.sageInk
                                    "error" -> p.danger
                                    else -> p.text2
                                }
                                Text(tailcatText, color = tcColor, fontSize = PebbleFont.body)
                            }
                            CardDivider()
                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                Text(store.t("tsHostStatus"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                val hColor = when (store.hostTailscale?.state) {
                                    "running" -> p.sageInk
                                    "error" -> p.danger
                                    else -> p.text2
                                }
                                Text(hostTsText, color = hColor, fontSize = PebbleFont.body)
                            }
                            CardDivider()
                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                Text(store.t("tokenProtect"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(
                                    text = if (store.tailcat?.hasToken == true) store.t("tokenEnabled") else store.t("tokenUnset"),
                                    color = p.text,
                                    fontSize = PebbleFont.body,
                                )
                            }
                        }
                    }

                    SectionHeader(store.t("connectedNodes"))
                    SectionCard {
                        Column(
                            modifier = Modifier.padding(14.dp),
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            if (store.nodes.isEmpty()) {
                                Text(store.t("noRemoteNodes"), color = p.text2, fontSize = PebbleFont.secondary)
                            } else {
                                val labels = mapOf(
                                    "files" to store.t("permFiles"),
                                    "exec" to store.t("permExec"),
                                    "desktop" to store.t("permDesktop"),
                                )
                                store.nodes.forEachIndexed { idx, node ->
                                    if (idx > 0) CardDivider()
                                    val on = node.connected == true
                                    val perms = node.permissions.orEmpty()
                                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                            Text(node.name ?: node.id, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                                            TagPill(
                                                text = store.t(if (on) "online" else "offline"),
                                                bg = if (on) p.sage else p.s2,
                                                fg = if (on) p.sageInk else p.text2,
                                            )
                                        }
                                        val host = node.hostname?.takeIf { it.isNotEmpty() }?.let { " · $it" }.orEmpty()
                                        Text("${node.os.orEmpty()}/${node.arch.orEmpty()}$host", color = p.text2, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
                                        if (perms.isNotEmpty()) {
                                            Row(
                                                modifier = Modifier.fillMaxWidth(),
                                                verticalAlignment = Alignment.CenterVertically,
                                                horizontalArrangement = Arrangement.spacedBy(6.dp),
                                            ) {
                                                perms.forEach { perm ->
                                                    TagPill(labels[perm] ?: perm, p.s1, p.text2)
                                                }
                                                Spacer(Modifier.weight(1f))
                                                if (on && "desktop" in perms) {
                                                    OutlinedButton(
                                                        onClick = { onOpenDesktop(node.id) },
                                                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                                    ) {
                                                        Text(store.t("openDesktop"), fontSize = PebbleFont.secondary, color = p.text)
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    Spacer(Modifier.height(16.dp))
                }
            }
        }
    }
}
