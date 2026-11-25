package ai.pebble.android.ui.connect

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import ai.pebble.android.data.ConnState
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.ServerProfile
import ai.pebble.android.data.TailscalePeer
import ai.pebble.android.data.errText
import ai.pebble.android.data.normalizeAddress
import ai.pebble.android.notify.NotificationHelper
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PEBBLE_COLOR_HEX
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionFooter
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import ai.pebble.android.ui.theme.readClipboardText
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

private const val DESKTOP_DOWNLOAD_URL = "https://github.com/pebble-ai/bot-hub/releases/latest"

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConnectScreen(
    store: PebbleStore,
    isGate: Boolean,
    startScan: Boolean = false,
    onClose: (() -> Unit)? = null,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var scanning by remember { mutableStateOf(startScan) }
    var label by remember { mutableStateOf("") }
    var address by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var standbyAddress by remember { mutableStateOf("") }
    var standbyToken by remember { mutableStateOf("") }
    var err by remember { mutableStateOf("") }
    var infoHint by remember { mutableStateOf("") }
    var submitting by remember { mutableStateOf(false) }
    var showFormWhileConnecting by remember { mutableStateOf(false) }
    var confirm by remember { mutableStateOf<ConfirmItem?>(null) }
    var tsAdvOpen by remember { mutableStateOf(false) }
    var tsAuthKey by remember { mutableStateOf("") }
    var tsControlUrl by remember { mutableStateOf(store.tsControlUrl) }
    var manualOpen by remember { mutableStateOf(false) }
    var manualAdvOpen by remember { mutableStateOf(false) }
    var unboxPromptLaunched by remember { mutableStateOf(false) }

    val multiPermLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        store.markUnboxPermsDone()
    }

    val isFreshOnboarding = isGate && store.servers.isEmpty() && store.activeServer == null
    val showFreshHero = isFreshOnboarding && !manualOpen
    val isConnectingGate = isGate && store.activeServer != null &&
        store.connState == ConnState.CONNECTING && store.bots.isEmpty() &&
        !showFormWhileConnecting && !submitting

    LaunchedEffect(isFreshOnboarding, store.unboxPermsDone) {
        if (isFreshOnboarding && !store.unboxPermsDone && !unboxPromptLaunched) {
            unboxPromptLaunched = true
            val reqs = buildList {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                    !NotificationHelper.hasNotificationPermission(context)
                ) {
                    add(Manifest.permission.POST_NOTIFICATIONS)
                }
                if (ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
                    add(Manifest.permission.CAMERA)
                }
            }
            if (reqs.isNotEmpty()) {
                delay(350)
                multiPermLauncher.launch(reqs.toTypedArray())
            } else {
                store.markUnboxPermsDone()
            }
        }
    }

    LaunchedEffect(Unit) {
        val active = store.activeServer
        if (active != null && (store.connState == ConnState.UNAUTHORIZED || store.connState == ConnState.ERROR)) {
            if (label.isEmpty()) label = active.label
            if (address.isEmpty()) address = active.address
        }
        if (tsControlUrl.isEmpty()) tsControlUrl = store.tsControlUrl
        if (store.tsBound && store.mobileTailscale == null) {
            store.refreshMobileTailscale(includePeers = true)
        }
    }

    LaunchedEffect(store.connState) {
        when (store.connState) {
            ConnState.CONNECTED -> {
                submitting = false
                showFormWhileConnecting = false
                if (!isGate) onClose?.invoke()
            }
            ConnState.ERROR, ConnState.UNAUTHORIZED -> {
                if (submitting) {
                    submitting = false
                    err = store.connError.ifEmpty { store.t("connFailed") }
                }
            }
            else -> Unit
        }
    }

    fun connect(addr: String, tok: String) {
        submitting = true
        err = ""
        scope.launch {
            try {
                store.addAndConnectServer(
                    label = label,
                    address = addr,
                    token = tok,
                    standbyAddress = standbyAddress,
                    standbyToken = standbyToken,
                )
            } catch (e: Throwable) {
                submitting = false
                err = errText(e, store.t("connFailed"))
            }
        }
    }

    fun onScanned(raw: String) {
        val s = raw.trim()
        val lower = s.lowercase()
        if (!(lower.startsWith("tailcat://") || lower.startsWith("tailscale://") ||
                lower.startsWith("http://") || lower.startsWith("https://"))
        ) {
            err = store.t("invalidQr")
            return
        }
        scanning = false
        val norm = normalizeAddress(s)
        address = norm.cleanAddress
        if (norm.token.isNotEmpty()) token = norm.token
        connect(s, norm.token.ifEmpty { token })
    }

    fun submit() {
        err = ""
        if (address.trim().isEmpty()) {
            err = store.t("enterAddrOrScan")
            return
        }
        connect(address, token)
    }

    fun pastePairingLink() {
        err = ""
        val raw = readClipboardText(context).trim().replace(Regex("[\\r\\n]+"), "")
        if (raw.isEmpty()) {
            infoHint = store.t("clipboardEmpty")
            return
        }
        infoHint = ""
        address = raw
    }

    fun selectPeer(peer: TailscalePeer) {
        err = ""
        infoHint = ""
        label = peer.hostname
        address = peer.address
        val saved = store.servers.firstOrNull { it.address == peer.address && it.token.isNotEmpty() }
        if (saved != null) {
            token = saved.token
            connect(peer.address, saved.token)
            return
        }
        if (token.trim().isNotEmpty()) {
            connect(peer.address, token)
            return
        }
        manualAdvOpen = true
        infoHint = store.t("tsPeerSelectedHint")
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    if (isConnectingGate) {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(p.page)
                .windowInsetsPadding(WindowInsets.statusBars),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                CircularProgressIndicator(color = p.accent)
                Text(store.t("connectingHost"), color = p.text2, fontSize = PebbleFont.body)
                OutlinedButton(onClick = { showFormWhileConnecting = true }) {
                    Text(store.t("switchHost"), color = p.text)
                }
            }
        }
        return
    }

    if (showFreshHero) {
        BoxWithConstraints(
            modifier = Modifier
                .fillMaxSize()
                .background(p.page)
                .windowInsetsPadding(WindowInsets.statusBars)
                .navigationBarsPadding(),
        ) {
            val minH = maxHeight
            val heroScannerHeight = (minH - 210.dp).coerceIn(380.dp, 600.dp)
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState()),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Column(
                    modifier = Modifier
                        .widthIn(max = if (scanning) 512.dp else 408.dp)
                        .fillMaxWidth()
                        .heightIn(min = minH)
                        .padding(
                            horizontal = if (scanning) 16.dp else 24.dp,
                            vertical = if (scanning) 12.dp else 20.dp,
                        ),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = if (scanning) Arrangement.Center else Arrangement.Top,
                ) {
                    if (!scanning) {
                        Spacer(Modifier.weight(1f))
                    } else {
                        Spacer(Modifier.height(4.dp))
                    }

                    AnimatedVisibility(visible = !scanning) {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(14.dp),
                        ) {
                            BotAvatar(
                                id = "assistant",
                                size = 104.dp,
                                color = PEBBLE_COLOR_HEX,
                                busy = submitting,
                                interactive = true,
                            )
                            Text(
                                text = "Pebble",
                                color = p.text,
                                fontSize = PebbleFont.hero,
                                fontWeight = FontWeight.Bold,
                            )
                            Text(
                                text = store.t("onboardHeroSub"),
                                color = p.text2,
                                fontSize = PebbleFont.body,
                                textAlign = TextAlign.Center,
                            )
                        }
                    }

                    if (!scanning) {
                        Spacer(Modifier.weight(1f))
                    }

                    AnimatedVisibility(visible = scanning) {
                        QrScannerBox(
                            onCode = { onScanned(it) },
                            onMessage = { err = it },
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(heroScannerHeight)
                                .padding(bottom = 14.dp),
                        )
                    }

                    Column(
                        modifier = Modifier.fillMaxWidth(),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Button(
                            onClick = {
                                err = ""
                                scanning = !scanning
                            },
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(48.dp),
                            shape = RoundedCornerShape(14.dp),
                            colors = ButtonDefaults.buttonColors(
                                containerColor = p.accent,
                                contentColor = p.onAccent,
                            ),
                        ) {
                            Icon(painterResource(PebbleIcons.QrCodeScanner), contentDescription = null, modifier = Modifier.size(18.dp))
                            Spacer(Modifier.size(8.dp))
                            Text(
                                text = if (scanning) store.t("collapse") else store.t("scanConnect"),
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }

                        Button(
                            onClick = {
                                scanning = false
                                manualOpen = true
                            },
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(48.dp),
                            shape = RoundedCornerShape(14.dp),
                            colors = ButtonDefaults.buttonColors(
                                containerColor = p.card,
                                contentColor = p.text,
                            ),
                        ) {
                            Text(
                                text = store.t("manualPairingBtn"),
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.Medium,
                            )
                        }
                    }

                    if (err.isNotEmpty()) {
                        Spacer(Modifier.height(12.dp))
                        Text(
                            text = err,
                            color = p.danger,
                            fontSize = PebbleFont.secondary,
                            textAlign = TextAlign.Center,
                        )
                    } else if (infoHint.isNotEmpty()) {
                        Spacer(Modifier.height(12.dp))
                        Text(
                            text = infoHint,
                            color = p.sageInk,
                            fontSize = PebbleFont.secondary,
                            textAlign = TextAlign.Center,
                        )
                    }

                    Spacer(Modifier.height(if (scanning) 14.dp else 22.dp))

                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Text(store.t("noDesktopYet"), color = p.mute, fontSize = PebbleFont.caption)
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                text = store.t("downloadDesktop"),
                                color = p.text2,
                                fontSize = PebbleFont.caption,
                                fontWeight = FontWeight.Medium,
                                modifier = Modifier.clickable {
                                    runCatching {
                                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(DESKTOP_DOWNLOAD_URL)))
                                    }
                                },
                            )
                            Text("·", color = p.mute, fontSize = PebbleFont.caption)
                            Text(
                                text = store.t("sendToComputer"),
                                color = p.text2,
                                fontSize = PebbleFont.caption,
                                fontWeight = FontWeight.Medium,
                                modifier = Modifier.clickable {
                                    val share = Intent(Intent.ACTION_SEND).apply {
                                        type = "text/plain"
                                        putExtra(Intent.EXTRA_TEXT, DESKTOP_DOWNLOAD_URL)
                                    }
                                    context.startActivity(Intent.createChooser(share, store.t("sendToComputer")))
                                },
                            )
                        }
                    }
                }
            }
        }
        return
    }

    // Form Content (gate manual mode or modal management sheet)
    val norm = remember(address) { normalizeAddress(address) }

    Scaffold(
        containerColor = p.page,
        topBar = {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.page,
                    titleContentColor = p.text,
                ),
                title = {
                    Text(
                        text = if (isGate) "Pebble" else store.t("connectHost"),
                        fontSize = PebbleFont.title,
                        fontWeight = FontWeight.Bold,
                    )
                },
                navigationIcon = {
                    if (isGate && isFreshOnboarding && manualOpen) {
                        IconButton(onClick = { manualOpen = false }) {
                            Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("backBtn"), tint = p.text)
                        }
                    } else if (!isGate && onClose != null) {
                        IconButton(onClick = onClose) {
                            Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                        }
                    }
                },
                actions = {
                    if (submitting) {
                        CircularProgressIndicator(
                            modifier = Modifier
                                .padding(end = 16.dp)
                                .size(20.dp),
                            strokeWidth = 2.dp,
                            color = p.accent,
                        )
                    } else {
                        TextButton(onClick = { submit() }) {
                            Text(
                                text = store.t("connect"),
                                color = p.accent,
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                },
            )
        },
    ) { innerPadding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .imePadding(),
            contentAlignment = Alignment.TopCenter,
        ) {
            Column(
                modifier = Modifier
                    .widthIn(max = 580.dp)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                if (isGate && store.activeServer != null &&
                    (store.connState == ConnState.UNAUTHORIZED || store.connState == ConnState.ERROR)
                ) {
                    SectionHeader(
                        if (store.connState == ConnState.UNAUTHORIZED) store.t("tokenRequired") else store.t("cantConnectHost")
                    )
                    SectionCard {
                        Column(
                            modifier = Modifier.padding(16.dp),
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            Text(
                                text = if (store.connState == ConnState.ERROR && store.connError.isNotEmpty()) {
                                    store.connError
                                } else {
                                    store.t("onboardDesc")
                                },
                                color = p.text2,
                                fontSize = PebbleFont.body,
                            )
                            OutlinedButton(
                                onClick = {
                                    err = ""
                                    showFormWhileConnecting = false
                                    scope.launch { store.reconnect() }
                                },
                            ) {
                                Text(store.t("retry"), color = p.text)
                            }
                        }
                    }
                }

                // Scan QR Section
                SectionHeader(store.t("scanPairTitle"))
                SectionCard {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                err = ""
                                scanning = !scanning
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Icon(painterResource(PebbleIcons.QrCodeScanner), contentDescription = null, tint = p.accent)
                        Text(
                            text = if (scanning) store.t("collapse") else store.t("scanConnect"),
                            color = p.text,
                            fontSize = PebbleFont.cardTitle,
                            fontWeight = FontWeight.Medium,
                        )
                    }
                    AnimatedVisibility(visible = scanning) {
                        Column(modifier = Modifier.fillMaxWidth()) {
                            HorizontalDivider(thickness = 0.5.dp, color = p.s2)
                            QrScannerBox(
                                onCode = { onScanned(it) },
                                onMessage = { err = it },
                                shape = RectangleShape,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .height(420.dp),
                            )
                        }
                    }
                }
                SectionFooter(store.t("scanPairDesc"))

                // Manual / Paste Section
                SectionHeader(store.t("manualPairingBtn"))
                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        OutlinedTextField(
                            value = address,
                            onValueChange = { address = it },
                            placeholder = { Text(store.t("pairingLinkPh"), fontFamily = MonoFont) },
                            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                            minLines = 2,
                            maxLines = 5,
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .clickable { pastePairingLink() }
                                .padding(horizontal = 4.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Icon(painterResource(PebbleIcons.ContentPaste), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                            Text(store.t("pasteLinkBtn"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                        }

                        CardDivider()

                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .clickable { manualAdvOpen = !manualAdvOpen }
                                .padding(horizontal = 4.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                text = store.t("moreOptions"),
                                color = p.text2,
                                fontSize = PebbleFont.secondary,
                                modifier = Modifier.weight(1f),
                            )
                            Icon(
                                painter = painterResource(if (manualAdvOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                                contentDescription = null,
                                tint = p.mute,
                            )
                        }

                        AnimatedVisibility(visible = manualAdvOpen) {
                            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                OutlinedTextField(
                                    value = label,
                                    onValueChange = { label = it },
                                    label = { Text(store.t("labelOptional"), fontSize = PebbleFont.secondary) },
                                    placeholder = { Text(store.t("labelExamplePh")) },
                                    singleLine = true,
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.fillMaxWidth(),
                                )
                                OutlinedTextField(
                                    value = token,
                                    onValueChange = { token = it },
                                    label = { Text(store.t("tokenOptionalLbl"), fontSize = PebbleFont.secondary) },
                                    placeholder = { Text(store.t("tokenFillHint")) },
                                    singleLine = true,
                                    visualTransformation = PasswordVisualTransformation(),
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.fillMaxWidth(),
                                )
                                OutlinedTextField(
                                    value = standbyAddress,
                                    onValueChange = { standbyAddress = it },
                                    label = { Text(store.t("standbyAddress"), fontSize = PebbleFont.secondary) },
                                    placeholder = { Text("tailcat://… / 192.168.x.x:3789", fontFamily = MonoFont) },
                                    singleLine = true,
                                    textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.fillMaxWidth(),
                                )
                                OutlinedTextField(
                                    value = standbyToken,
                                    onValueChange = { standbyToken = it },
                                    label = { Text(store.t("standbyToken"), fontSize = PebbleFont.secondary) },
                                    placeholder = { Text(store.t("tokenFillHint")) },
                                    singleLine = true,
                                    visualTransformation = PasswordVisualTransformation(),
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.fillMaxWidth(),
                                )
                            }
                        }
                    }
                }
                SectionFooter(
                    text = when {
                        infoHint.isNotEmpty() -> infoHint
                        norm.token.isNotEmpty() -> store.t("tokenRecognized")
                        else -> store.t("pasteLinkHint")
                    },
                    color = if (infoHint.isNotEmpty() || norm.token.isNotEmpty()) p.sageInk else p.mute,
                )

                if (err.isNotEmpty()) {
                    SectionCard {
                        Box(modifier = Modifier.padding(14.dp)) {
                            StatusTextBanner(text = err, error = true)
                        }
                    }
                }

                // Tailscale Network
                val ts = store.mobileTailscale
                val tsState = ts?.state ?: "disabled"
                SectionHeader(store.t("tsNetwork"))
                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        when {
                            tsState == "running" -> {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(
                                            text = store.t("tsBoundReady", mapOf("tailnet" to (ts?.tailnet ?: "Tailscale"))),
                                            color = p.sageInk,
                                            fontSize = PebbleFont.body,
                                            fontWeight = FontWeight.SemiBold,
                                        )
                                        val ip = ts?.ip
                                        if (!ip.isNullOrEmpty()) {
                                            Text(ip, color = p.text2, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
                                        }
                                    }
                                    TextButton(
                                        onClick = { scope.launch { store.refreshMobileTailscale(includePeers = true) } },
                                        contentPadding = PaddingValues(horizontal = 8.dp),
                                    ) {
                                        Text(store.t("tsRefreshPeers"), fontSize = PebbleFont.secondary, color = p.accent)
                                    }
                                    TextButton(
                                        onClick = {
                                            confirm = ConfirmItem(
                                                title = store.t("tsUnlinkTitle"),
                                                message = store.t("tsUnlinkDesc"),
                                            ) {
                                                scope.launch { store.logoutTailscale() }
                                            }
                                        },
                                        contentPadding = PaddingValues(horizontal = 8.dp),
                                    ) {
                                        Text(store.t("tsUnlink"), fontSize = PebbleFont.secondary, color = p.danger)
                                    }
                                }

                                val peers = ts?.peers.orEmpty().filter { it.online }
                                if (peers.isEmpty()) {
                                    Text(store.t("tsNoPeers"), color = p.text2, fontSize = PebbleFont.secondary)
                                } else {
                                    peers.forEachIndexed { idx, peer ->
                                        if (idx > 0) CardDivider()
                                        Row(
                                            modifier = Modifier
                                                .fillMaxWidth()
                                                .clip(PebbleDimens.rowShape)
                                                .clickable { selectPeer(peer) }
                                                .padding(vertical = 8.dp, horizontal = 4.dp),
                                            verticalAlignment = Alignment.CenterVertically,
                                        ) {
                                            Column(modifier = Modifier.weight(1f)) {
                                                Row(
                                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                                    verticalAlignment = Alignment.CenterVertically,
                                                ) {
                                                    Text(peer.hostname, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                                                    if (peer.isBotHub) {
                                                        TagPill(store.t("tsBotHubBadge"), p.lilac, p.lilacInk)
                                                    }
                                                }
                                                Text(
                                                    text = peer.dnsName ?: peer.ip ?: peer.address,
                                                    color = p.text2,
                                                    fontSize = PebbleFont.caption,
                                                    fontFamily = MonoFont,
                                                    maxLines = 1,
                                                    overflow = TextOverflow.Ellipsis,
                                                )
                                            }
                                            Icon(painterResource(PebbleIcons.ArrowOutward), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                        }
                                    }
                                }
                            }
                            tsState == "needs_login" -> {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Icon(painterResource(PebbleIcons.VpnKey), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    Spacer(Modifier.size(8.dp))
                                    Text(store.t("tsNeedsLogin"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                    val auth = ts?.authUrl
                                    if (!auth.isNullOrBlank()) {
                                        Button(
                                            onClick = {
                                                runCatching {
                                                    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(auth)))
                                                }
                                            },
                                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                        ) {
                                            Text(store.t("tsOpenLogin"), fontSize = PebbleFont.secondary)
                                        }
                                    }
                                }
                            }
                            tsState == "starting" || store.tsBinding -> {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = p.accent)
                                    Text(store.t("tsBinding"), color = p.text2, fontSize = PebbleFont.body)
                                }
                            }
                            else -> {
                                val tsErr = ts?.error
                                if (tsState == "error" && !tsErr.isNullOrBlank()) {
                                    StatusTextBanner(text = tsErr, error = true)
                                }
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable {
                                            scope.launch {
                                                store.bindTailscale(authKey = tsAuthKey, controlUrl = tsControlUrl)
                                                tsAuthKey = ""
                                            }
                                        }
                                        .padding(vertical = 8.dp, horizontal = 4.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    Icon(painterResource(PebbleIcons.Hub), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    Text(store.t("tsBindAccount"), color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                                }
                                CardDivider()
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable { tsAdvOpen = !tsAdvOpen }
                                        .padding(vertical = 6.dp, horizontal = 4.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Text(store.t("tsAdvanced"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                                    Icon(
                                        painter = painterResource(if (tsAdvOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                                        contentDescription = null,
                                        tint = p.mute,
                                    )
                                }
                                AnimatedVisibility(visible = tsAdvOpen) {
                                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                        OutlinedTextField(
                                            value = tsAuthKey,
                                            onValueChange = { tsAuthKey = it },
                                            placeholder = { Text(store.t("tsAuthKeyPh"), fontFamily = MonoFont) },
                                            singleLine = true,
                                            visualTransformation = PasswordVisualTransformation(),
                                            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                                            shape = PebbleDimens.rowShape,
                                            colors = pebbleFieldColors(),
                                            modifier = Modifier.fillMaxWidth(),
                                        )
                                        OutlinedTextField(
                                            value = tsControlUrl,
                                            onValueChange = { tsControlUrl = it },
                                            placeholder = { Text(store.t("tsControlUrlPh"), fontFamily = MonoFont) },
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
                    }
                }

                // Saved Hosts
                if (store.servers.isNotEmpty()) {
                    SectionHeader(store.t("savedHosts"))
                    SectionCard {
                        store.servers.forEachIndexed { idx, s ->
                            if (idx > 0) CardDivider()
                            SavedHostRow(
                                server = s,
                                isActive = s.id == store.activeServerId,
                                onSelect = {
                                    err = ""
                                    submitting = true
                                    scope.launch { store.selectServer(s.id) }
                                },
                                onDelete = {
                                    confirm = ConfirmItem(title = store.t("removeBtn"), message = s.label) {
                                        scope.launch { store.removeServer(s.id) }
                                    }
                                },
                            )
                        }
                    }
                }
                Spacer(Modifier.height(16.dp))
            }
        }
    }
}

@Composable
private fun SavedHostRow(
    server: ServerProfile,
    isActive: Boolean,
    onSelect: () -> Unit,
    onDelete: () -> Unit,
) {
    val p = LocalPalette.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onSelect)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(server.label, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.Medium)
            Text(
                text = server.address,
                color = p.text2,
                fontSize = PebbleFont.secondary,
                fontFamily = MonoFont,
                maxLines = 1,
                overflow = TextOverflow.MiddleEllipsis,
            )
        }
        if (isActive) {
            Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
        }
        IconButton(onClick = onDelete) {
            Icon(painterResource(PebbleIcons.DeleteOutline), contentDescription = null, tint = p.mute, modifier = Modifier.size(19.dp))
        }
    }
}
