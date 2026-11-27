package ai.pebble.android.ui.settings

import android.content.Intent
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.ConnState
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionFooter
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.copyToClipboard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun NetworkSettingsTab(
    store: PebbleStore,
    onOpenConnect: () -> Unit,
    onOpenDesktop: (String) -> Unit,
    onError: (String) -> Unit,
    onSaved: (String) -> Unit,
    onConfirm: (ConfirmItem) -> Unit,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var tcQRBytes by remember { mutableStateOf<ByteArray?>(null) }
    var tcQRVersion by remember { mutableIntStateOf(0) }
    var tcTokenIn by remember { mutableStateOf("") }
    var tcTokenVal by remember { mutableStateOf("") }
    var tcTokenShow by remember { mutableStateOf(false) }

    var tsQRBytes by remember { mutableStateOf<ByteArray?>(null) }
    var tsQRVersion by remember { mutableIntStateOf(0) }
    var tsAdvOpen by remember { mutableStateOf(false) }
    var tsHostnameIn by remember { mutableStateOf("") }
    var tsAuthKeyIn by remember { mutableStateOf("") }
    var tsControlUrlIn by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf("") }

    suspend fun loadAll() {
        store.refreshMobileTailscale(includePeers = false)
        if (!store.isConnected) return
        coroutineScope {
            val n = async { runCatching { store.refreshNodes() } }
            val tc = async { runCatching { store.refreshTailcat() } }
            val ht = async { runCatching { store.refreshHostTailscale() } }
            n.await(); tc.await(); ht.await()
        }
        if (tsHostnameIn.isEmpty()) store.hostTailscale?.hostname?.takeIf { it.isNotEmpty() }?.let { tsHostnameIn = it }
        if (tsControlUrlIn.isEmpty()) store.hostTailscale?.controlUrl?.takeIf { it.isNotEmpty() }?.let { tsControlUrlIn = it }
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

    LaunchedEffect(store.isConnected) {
        loadAll()
    }

    LaunchedEffect(store.tailcat?.state, store.tailcat?.link, tcQRVersion, store.isConnected) {
        tcQRBytes = if (store.isConnected && store.tailcat?.state == "ready") {
            runCatching { store.fetchTailcatQR() }.getOrNull()
        } else {
            null
        }
    }

    LaunchedEffect(store.hostTailscale?.state, store.hostTailscale?.link, tsQRVersion, store.isConnected) {
        tsQRBytes = if (store.isConnected && store.hostTailscale?.state == "running") {
            runCatching { store.fetchHostTailscaleQR() }.getOrNull()
        } else {
            null
        }
    }

    LaunchedEffect(store.tailcat?.state, store.hostTailscale?.state, store.isConnected) {
        val tcBusy = store.tailcat?.state == "connecting"
        val tsBusy = store.hostTailscale?.state == "starting" || store.hostTailscale?.state == "needs_login"
        if (!store.isConnected || (!tcBusy && !tsBusy)) return@LaunchedEffect
        while (true) {
            delay(1500)
            if (tcBusy) runCatching { store.refreshTailcat() }
            if (tsBusy) runCatching { store.refreshHostTailscale() }
        }
    }

    val hostColor = when (store.connState) {
        ConnState.CONNECTED -> if (store.online) p.sageInk else p.busy
        ConnState.CONNECTING -> p.busy
        ConnState.UNAUTHORIZED, ConnState.ERROR -> p.danger
        ConnState.IDLE -> p.mute
    }
    val hostText = when (val s = store.activeServer) {
        null -> store.t("disconnected")
        else -> when (store.connState) {
            ConnState.CONNECTING -> "${s.label} · ${store.t("connectingShort")}"
            ConnState.UNAUTHORIZED -> "${s.label} · ${store.t("needsToken")}"
            ConnState.ERROR -> "${s.label} · ${store.t("connLost")}"
            ConnState.CONNECTED -> if (store.online) s.label else "${s.label} · ${store.t("reconnecting")}"
            ConnState.IDLE -> store.t("disconnected")
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

    // Section 1: Current Connection
    SectionHeader(store.t("currentMobileConn"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val active = store.activeServer
            if (active != null) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable(onClick = onOpenConnect)
                        .padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Box(modifier = Modifier.size(8.dp).clip(CircleShape).background(hostColor))
                    Text(hostText, color = p.text, fontSize = PebbleFont.body, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text(store.t("switchHost"), color = p.accent, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(store.t("connAddr"), color = p.text2, fontSize = PebbleFont.secondary)
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
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
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
                if (store.isClientOutdated) {
                    Text(
                        text = store.t(if (store.isClientUpgradeRequired) "clientUpgradeRequired" else "clientUpdateHint"),
                        color = if (store.isClientUpgradeRequired) p.pinkInk else p.text2,
                        fontSize = PebbleFont.secondary,
                    )
                }
            } else {
                Text(store.t("notConnectedHost"), color = p.mute, fontSize = PebbleFont.body)
                Text(
                    text = store.t("manageConn"),
                    color = p.accent,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.fillMaxWidth().clickable(onClick = onOpenConnect).padding(vertical = 4.dp),
                )
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

            if (active != null) {
                CardDivider()
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { onOpenDesktop(store.takeovers.firstOrNull()?.target ?: "local") }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Icon(painterResource(PebbleIcons.DesktopWindows), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                    val lbl = if (store.takeovers.isEmpty()) store.t("openHostDesktop") else "${store.t("openHostDesktop")} (${store.takeovers.size})"
                    Text(lbl, color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                }
            }
        }
    }

    // Section 2: Host Tailcat
    val tcState = store.tailcat?.state ?: "disabled"
    val tcIsOn = tcState == "ready" || tcState == "connecting"
    val hasTok = store.tailcat?.hasToken == true
    val envRef = store.tailcat?.tokenEnvRef.orEmpty()
    val tcStatusPair = when (tcState) {
        "ready" -> store.t("tailcatReady") to p.sageInk
        "connecting" -> store.t("tailcatConnecting") to p.busy
        "disabled" -> store.t("tailcatDisabled") to p.text2
        "error" -> (store.tailcat?.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")) to p.danger
        else -> store.t("tailcatUnknown") to p.text2
    }
    val tcFooter = when (tcState) {
        "ready" -> store.t("remoteHelpReady")
        "connecting" -> store.t("remoteHelpConnecting")
        "error" -> store.tailcat?.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")
        else -> store.t("remoteHelpOff")
    }

    SectionHeader(store.t("hostRemoteAccess"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(store.t("tunnelState"), color = p.text, fontSize = PebbleFont.body)
                Text("·", color = p.mute, fontSize = PebbleFont.body)
                Text(tcStatusPair.first, color = tcStatusPair.second, fontSize = PebbleFont.body, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (busy == "tc:toggle") {
                    CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = p.accent)
                }
                PebbleSwitch(
                    checked = tcIsOn,
                    enabled = store.isConnected && busy.isEmpty(),
                    onCheckedChange = { next ->
                        if (next) {
                            runOp("tc:toggle", "opFailed") {
                                val body = mutableMapOf<String, Any?>("enabled" to true)
                                val tok = tcTokenIn.trim()
                                if (tok.isNotEmpty()) body["apiToken"] = tok
                                val res = store.setTailcat(body)
                                tcTokenIn = ""
                                res.apiToken?.takeIf { it.isNotEmpty() }?.let { issued ->
                                    tcTokenVal = issued
                                    tcTokenShow = true
                                }
                                tcQRVersion += 1
                                null
                            }
                        } else {
                            onConfirm(
                                ConfirmItem(
                                    title = store.t("remoteDisableTitle"),
                                    message = store.t("remoteDisableDesc"),
                                ) {
                                    runOp("tc:toggle", "opFailed") {
                                        store.setTailcat(mapOf("enabled" to false))
                                        tcQRBytes = null
                                        null
                                    }
                                },
                            )
                        }
                    },
                )
            }

            if (tcState == "ready") {
                QrImageCard(bytes = tcQRBytes)
                store.tailcat?.link?.takeIf { it.isNotEmpty() }?.let { link ->
                    SelectionContainer {
                        Text(link, color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                    }
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        Button(
                            onClick = {
                                copyToClipboard(context, link)
                                onSaved(store.t("pairLinkCopied"))
                            },
                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        ) {
                            Text(store.t("copyPairLink"), fontSize = PebbleFont.secondary)
                        }
                    }
                }
            }

            if (!tcIsOn && !hasTok) {
                OutlinedTextField(
                    value = tcTokenIn,
                    onValueChange = { tcTokenIn = it },
                    label = { Text(store.t("accessTokenOptional"), fontSize = PebbleFont.caption) },
                    placeholder = { Text(store.t("accessTokenPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                    singleLine = true,
                    textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            if (hasTok) {
                CardDivider()
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Text(
                            text = store.t(if (envRef.isEmpty()) "accessTokenReusable" else "accessTokenEnv"),
                            color = p.text,
                            fontSize = PebbleFont.body,
                            modifier = Modifier.weight(1f),
                        )
                        if (envRef.isEmpty()) {
                            OutlinedButton(
                                onClick = {
                                    if (tcTokenShow) {
                                        tcTokenShow = false
                                    } else if (tcTokenVal.isNotEmpty()) {
                                        tcTokenShow = true
                                    } else {
                                        runOp("tc:reveal", "opFailed") {
                                            tcTokenVal = store.revealTailcatToken()
                                            tcTokenShow = true
                                            null
                                        }
                                    }
                                },
                                enabled = busy != "tc:reveal",
                                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                            ) {
                                Text(store.t(if (tcTokenShow) "hide" else "show"), fontSize = PebbleFont.caption, color = p.text)
                            }
                            OutlinedButton(
                                onClick = {
                                    runOp("tc:copy", "opFailed") {
                                        val tok = tcTokenVal.ifEmpty { store.revealTailcatToken() }
                                        tcTokenVal = tok
                                        if (tok.isNotEmpty()) {
                                            copyToClipboard(context, tok)
                                            store.t("tokenCopied")
                                        } else {
                                            null
                                        }
                                    }
                                },
                                enabled = busy != "tc:copy",
                                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                            ) {
                                Text(store.t("copy"), fontSize = PebbleFont.caption, color = p.text)
                            }
                            OutlinedButton(
                                onClick = {
                                    onConfirm(
                                        ConfirmItem(
                                            title = store.t("resetTokenTitle"),
                                            message = store.t("resetTokenDesc"),
                                        ) {
                                            runOp("tc:reset", "opFailed") {
                                                val res = store.setTailcat(mapOf("resetToken" to true))
                                                res.apiToken?.takeIf { it.isNotEmpty() }?.let { issued ->
                                                    tcTokenVal = issued
                                                    tcTokenShow = true
                                                }
                                                tcQRVersion += 1
                                                store.t("tokenResetToast")
                                            }
                                        },
                                    )
                                },
                                enabled = busy != "tc:reset",
                                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                            ) {
                                Text(store.t("resetToken"), fontSize = PebbleFont.caption, color = p.danger)
                            }
                        }
                    }

                    if (envRef.isNotEmpty()) {
                        Text(store.t("accessTokenEnvHint", mapOf("ref" to envRef)), color = p.text2, fontSize = PebbleFont.caption)
                    } else {
                        SelectionContainer {
                            Text(
                                text = if (tcTokenShow && tcTokenVal.isNotEmpty()) tcTokenVal else "•".repeat(24),
                                color = p.text2,
                                fontSize = PebbleFont.caption,
                                fontFamily = MonoFont,
                            )
                        }
                    }
                }
            }
        }
    }
    SectionFooter(tcFooter)

    // Section 3: Host Tailscale
    val tsState = store.hostTailscale?.state ?: "disabled"
    val tsIsOn = tsState == "running" || tsState == "starting" || tsState == "needs_login"
    val authEnv = store.hostTailscale?.authKeyEnvRef.orEmpty()
    val tsStatusPair = when (tsState) {
        "running" -> (store.hostTailscale?.tailnet ?: store.t("tailcatReady")) to p.sageInk
        "starting" -> store.t("tsBinding") to p.busy
        "needs_login" -> store.t("tsNeedsLogin") to p.busy
        "disabled" -> store.t("tailcatDisabled") to p.text2
        "error" -> (store.hostTailscale?.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")) to p.danger
        else -> store.t("tailcatUnknown") to p.text2
    }
    val tsFooter = when (tsState) {
        "running" -> store.t("tsHelpReady", mapOf("tailnet" to (store.hostTailscale?.tailnet ?: "Tailscale")))
        "needs_login" -> store.t("tsHelpLogin")
        "starting" -> store.t("tsHelpStarting")
        "error" -> store.hostTailscale?.error?.takeIf { it.isNotEmpty() } ?: store.t("tailcatErr")
        else -> store.t("tsHelpOff")
    }

    SectionHeader(store.t("tsHostStatus"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(store.t("tsNetwork"), color = p.text, fontSize = PebbleFont.body)
                Text("·", color = p.mute, fontSize = PebbleFont.body)
                Text(tsStatusPair.first, color = tsStatusPair.second, fontSize = PebbleFont.body, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (busy == "ts:toggle") {
                    CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = p.accent)
                }
                PebbleSwitch(
                    checked = tsIsOn,
                    enabled = store.isConnected && busy.isEmpty(),
                    onCheckedChange = { next ->
                        if (next) {
                            runOp("ts:toggle", "opFailed") {
                                val body = mutableMapOf<String, Any?>("enabled" to true)
                                val hn = tsHostnameIn.trim()
                                val cu = tsControlUrlIn.trim()
                                val ak = tsAuthKeyIn.trim()
                                val tok = tcTokenIn.trim()
                                if (hn.isNotEmpty()) body["hostname"] = hn
                                if (cu.isNotEmpty()) body["controlUrl"] = cu
                                if (ak.isNotEmpty()) body["authKey"] = ak
                                if (tok.isNotEmpty()) body["apiToken"] = tok
                                val res = store.setHostTailscale(body)
                                tsAuthKeyIn = ""
                                res.apiToken?.takeIf { it.isNotEmpty() }?.let { issued ->
                                    tcTokenVal = issued
                                    tcTokenShow = true
                                }
                                tsQRVersion += 1
                                null
                            }
                        } else {
                            runOp("ts:toggle", "opFailed") {
                                store.setHostTailscale(mapOf("enabled" to false))
                                tsQRBytes = null
                                null
                            }
                        }
                    },
                )
            }

            if (tsState == "needs_login") {
                val auth = store.hostTailscale?.authUrl.orEmpty()
                if (auth.isNotEmpty()) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            onClick = { runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(auth))) } },
                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        ) {
                            Text(store.t("tsOpenLogin"), fontSize = PebbleFont.secondary)
                        }
                        OutlinedButton(
                            onClick = {
                                copyToClipboard(context, auth)
                                onSaved(store.t("tsLoginUrlCopied"))
                            },
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        ) {
                            Text(store.t("tsCopyLoginUrl"), fontSize = PebbleFont.secondary, color = p.text)
                        }
                    }
                }
            }

            if (tsState == "running") {
                QrImageCard(bytes = tsQRBytes)
                val dns = store.hostTailscale?.dnsName.orEmpty()
                val ip = store.hostTailscale?.ip.orEmpty()
                val hostDesc = dns.ifEmpty { ip }
                if (hostDesc.isNotEmpty()) {
                    val suffix = if (dns.isNotEmpty() && ip.isNotEmpty()) " ($ip)" else ""
                    SelectionContainer {
                        Text(hostDesc + suffix, color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                    }
                }
                store.hostTailscale?.link?.takeIf { it.isNotEmpty() }?.let { link ->
                    SelectionContainer {
                        Text(link, color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                    }
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        OutlinedButton(
                            onClick = {
                                onConfirm(
                                    ConfirmItem(
                                        title = store.t("tsLogoutTitle"),
                                        message = store.t("tsLogoutDesc"),
                                    ) {
                                        runOp("ts:logout", "opFailed") {
                                            store.setHostTailscale(mapOf("logout" to true))
                                            tsQRBytes = null
                                            null
                                        }
                                    },
                                )
                            },
                            enabled = busy != "ts:logout",
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        ) {
                            Text(store.t("tsLogout"), color = p.danger, fontSize = PebbleFont.secondary)
                        }
                        Spacer(Modifier.width(8.dp))
                        Button(
                            onClick = {
                                copyToClipboard(context, link)
                                onSaved(store.t("tsAddressCopied"))
                            },
                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                        ) {
                            Text(store.t("tsCopyLink"), fontSize = PebbleFont.secondary)
                        }
                    }
                }
            }

            CardDivider()
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { tsAdvOpen = !tsAdvOpen }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("tsAdvanced"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                Icon(painterResource(if (tsAdvOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
            }
            AnimatedVisibility(visible = tsAdvOpen) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = tsHostnameIn,
                        onValueChange = { tsHostnameIn = it },
                        label = { Text(store.t("tsHostname"), fontSize = PebbleFont.caption) },
                        placeholder = { Text(store.t("tsHostnamePh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                        singleLine = true,
                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = tsAuthKeyIn,
                        onValueChange = { tsAuthKeyIn = it },
                        enabled = authEnv.isEmpty(),
                        visualTransformation = PasswordVisualTransformation(),
                        label = { Text("Auth Key", fontSize = PebbleFont.caption) },
                        placeholder = { Text(store.t("tsAuthKeyPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                        singleLine = true,
                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = tsControlUrlIn,
                        onValueChange = { tsControlUrlIn = it },
                        label = { Text("Control URL", fontSize = PebbleFont.caption) },
                        placeholder = { Text(store.t("tsControlUrlPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                        singleLine = true,
                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
        }
    }
    SectionFooter(tsFooter)

    // Section 4: Connected Nodes
    SectionHeader(
        title = store.t("connectedNodes"),
        trailing = {
            IconButton(
                onClick = { scope.launch { loadAll() } },
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
            if (store.nodes.isEmpty()) {
                Text(store.t("noRemoteNodes"), color = p.text2, fontSize = PebbleFont.secondary)
            } else {
                val permLabels = mapOf(
                    "files" to store.t("permFiles"),
                    "exec" to store.t("permExec"),
                    "desktop" to store.t("permDesktop"),
                    "agent" to store.t("permAgent"),
                )
                store.nodes.forEachIndexed { idx, node ->
                    if (idx > 0) CardDivider()
                    val on = node.connected == true
                    val outdated = store.serverInfo != null && node.isOutdated
                    val perms = node.permissions.orEmpty()
                    val harnesses = node.harnesses.orEmpty()
                    val workspaces = node.workspaces.orEmpty()
                    val activeCount = node.activeTaskIds?.size ?: 0
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            Text(node.name ?: node.id, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            if (activeCount > 0) {
                                TagPill(store.t("nodeActiveTasks", mapOf("n" to activeCount)), p.sage, p.sageInk)
                            }
                            if (outdated) {
                                TagPill(store.t("nodeOutdatedTag"), p.pink, p.pinkInk)
                            }
                            TagPill(
                                text = store.t(if (on) "online" else "offline"),
                                bg = if (on) p.sage else p.s2,
                                fg = if (on) p.sageInk else p.text2,
                            )
                        }
                        val host = node.hostname?.takeIf { it.isNotEmpty() }?.let { " · $it" }.orEmpty()
                        Text("${node.os.orEmpty()}/${node.arch.orEmpty()}$host", color = p.text2, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
                        node.maxPermissionMode?.takeIf { it.isNotEmpty() }?.let { maxMode ->
                            val modeKey = "permMode_$maxMode"
                            val modeLabel = store.t(modeKey).let { if (it == modeKey) maxMode else it }
                            Text(store.t("nodeMaxPermMode", mapOf("mode" to modeLabel)), color = p.mute, fontSize = PebbleFont.caption)
                        }
                        if (outdated) {
                            Text(store.t("nodeOutdatedHint"), color = p.mute, fontSize = PebbleFont.caption)
                        }
                        if (perms.isNotEmpty() || (on && "desktop" in perms)) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(6.dp),
                            ) {
                                FlowRow(
                                    modifier = Modifier.weight(1f),
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                    verticalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    perms.forEach { perm ->
                                        val isAgent = perm == "agent"
                                        TagPill(
                                            text = permLabels[perm] ?: perm,
                                            bg = if (isAgent) p.lilac else p.s1,
                                            fg = if (isAgent) p.lilacInk else p.text2,
                                        )
                                    }
                                }
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
                        if (harnesses.isNotEmpty()) {
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(store.t("nodeHarnesses"), color = p.mute, fontSize = PebbleFont.caption, fontWeight = FontWeight.Medium)
                                FlowRow(
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                    verticalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    harnesses.forEach { h ->
                                        val installed = h.installed == true
                                        val ready = installed && h.authStatus == "ready"
                                        val needsLogin = installed && h.authStatus == "needs_login"
                                        val suffix = when {
                                            !installed -> store.t("nodeNotInstalled")
                                            ready -> store.t("nodeAuthReady")
                                            needsLogin -> store.t("nodeAuthNeedsLogin")
                                            else -> ""
                                        }
                                        val label = if (suffix.isEmpty()) h.title else "${h.title} · $suffix"
                                        TagPill(
                                            text = label,
                                            bg = if (ready) p.sage else if (needsLogin) p.pink else p.s1,
                                            fg = if (ready) p.sageInk else if (needsLogin) p.pinkInk else p.text2,
                                        )
                                    }
                                }
                            }
                        }
                        if (workspaces.isNotEmpty()) {
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(store.t("nodeWorkspaces"), color = p.mute, fontSize = PebbleFont.caption, fontWeight = FontWeight.Medium)
                                workspaces.forEach { ws ->
                                    Row(
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                                    ) {
                                        Text(
                                            text = ws.name.ifEmpty { ws.id },
                                            color = p.text,
                                            fontSize = PebbleFont.caption,
                                            fontWeight = FontWeight.SemiBold,
                                        )
                                        Text(
                                            text = ws.path,
                                            color = p.text2,
                                            fontFamily = MonoFont,
                                            fontSize = PebbleFont.caption,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.weight(1f, fill = false),
                                        )
                                        if (ws.isGit == true) {
                                            TagPill(ws.defaultBranch?.takeIf { it.isNotEmpty() } ?: "git", p.s1, p.text2)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun QrImageCard(bytes: ByteArray?) {
    val p = LocalPalette.current
    val bmp = remember(bytes) {
        bytes?.let { BitmapFactory.decodeByteArray(it, 0, it.size)?.asImageBitmap() }
    }
    Row(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.Center) {
        Surface(
            color = Color.White,
            shape = RoundedCornerShape(14.dp),
        ) {
            Box(
                modifier = Modifier.size(164.dp).padding(10.dp),
                contentAlignment = Alignment.Center,
            ) {
                if (bmp != null) {
                    Image(
                        bitmap = bmp,
                        contentDescription = null,
                        filterQuality = FilterQuality.None,
                        modifier = Modifier.size(144.dp),
                    )
                } else {
                    CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp, color = p.accent)
                }
            }
        }
    }
}
