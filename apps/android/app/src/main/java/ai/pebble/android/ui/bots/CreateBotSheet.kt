package ai.pebble.android.ui.bots

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.List
import androidx.compose.material.icons.rounded.ArrowDropDown
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.theme.BotAppearancePicker
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CreateBotSheet(
    store: PebbleStore,
    onClose: () -> Unit,
    onCreated: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var name by remember { mutableStateOf("") }
    var botId by remember { mutableStateOf("") }
    var idTouched by remember { mutableStateOf(false) }
    var desc by remember { mutableStateOf("") }
    var avatar by remember { mutableStateOf("") }
    var color by remember { mutableStateOf("") }
    var harness by remember { mutableStateOf("opencode") }
    var harnessTouched by remember { mutableStateOf(false) }
    var model by remember { mutableStateOf("") }
    var loadingModels by remember { mutableStateOf(false) }
    var instructions by remember { mutableStateOf("") }
    var submitting by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf("") }

    var harnessMenuOpen by remember { mutableStateOf(false) }
    var modelMenuOpen by remember { mutableStateOf(false) }

    val harnesses = remember(store.settings?.harnesses) {
        val list = store.settings?.harnesses.orEmpty()
        list.ifEmpty {
            listOf("opencode", "codebuddy", "qoder", "codex", "claude", "grok", "pi").map { HarnessInfo(id = it, label = it) }
        }
    }

    fun slug(s: String): String =
        s.lowercase()
            .replace(Regex("[^a-z0-9_-]+"), "-")
            .replace(Regex("^-+|-+$"), "")

    fun loadCands(target: String) {
        if (loadingModels) return
        loadingModels = true
        scope.launch {
            runCatching { store.loadModels(target) }
            loadingModels = false
        }
    }

    LaunchedEffect(Unit) {
        if (store.settings == null) runCatching { store.loadSettings() }
        if (!harnessTouched) {
            harness = store.settings?.defaultHarness ?: harnesses.firstOrNull()?.id ?: "opencode"
        }
    }

    LaunchedEffect(harness) {
        if (store.isConnected && store.models[harness].isNullOrEmpty()) {
            loadCands(harness)
        }
    }

    fun create() {
        err = ""
        val n = name.trim()
        val id = botId.trim()
        if (n.isEmpty()) {
            err = store.t("fillBotName")
            return
        }
        if (!Regex("^[a-zA-Z0-9][a-zA-Z0-9_-]*$").matches(id)) {
            err = store.t("idInvalid")
            return
        }
        val body = mutableMapOf<String, Any?>("id" to id, "name" to n, "harness" to harness)
        val d = desc.trim()
        val m = model.trim()
        val ins = instructions.trim()
        if (d.isNotEmpty()) body["description"] = d
        if (avatar.isNotEmpty()) body["avatar"] = avatar
        if (color.isNotEmpty()) body["color"] = color
        if (m.isNotEmpty()) body["model"] = m
        if (ins.isNotEmpty()) body["instructions"] = ins

        submitting = true
        scope.launch {
            try {
                val st = store.createBot(body)
                onCreated(st.id)
            } catch (e: Throwable) {
                err = errText(e, store.t("createFailed"))
                submitting = false
            }
        }
    }

    val previewId = botId.ifEmpty { name.ifEmpty { "new" } }
    val ph = resolveDefaultModel(store.settings?.defaultModel.orEmpty(), harness)
    val cands = store.models[harness].orEmpty()

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
                    Text(store.t("newBot"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
                actions = {
                    if (submitting) {
                        CircularProgressIndicator(modifier = Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp, color = p.accent)
                    } else {
                        TextButton(onClick = { create() }) {
                            Text(store.t("create"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
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
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 8.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    BotAvatar(id = previewId, size = 68.dp, avatar = avatar, color = color, interactive = true)
                }

                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        OutlinedTextField(
                            value = name,
                            onValueChange = {
                                name = it
                                if (!idTouched) botId = slug(it)
                            },
                            label = { Text(store.t("botName"), fontSize = PebbleFont.secondary) },
                            placeholder = { Text(store.t("botNameExamplePh")) },
                            singleLine = true,
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = botId,
                            onValueChange = {
                                idTouched = true
                                botId = it
                            },
                            label = { Text("ID", fontSize = PebbleFont.secondary) },
                            placeholder = { Text(store.t("botIdPh"), fontFamily = MonoFont) },
                            singleLine = true,
                            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = desc,
                            onValueChange = { desc = it },
                            label = { Text(store.t("descOptional"), fontSize = PebbleFont.secondary) },
                            placeholder = { Text(store.t("botDescCreatePh")) },
                            singleLine = true,
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }

                SectionHeader(store.t("appearance"))
                SectionCard {
                    Box(modifier = Modifier.padding(14.dp)) {
                        BotAppearancePicker(
                            botId = previewId,
                            avatar = avatar,
                            color = color,
                            onAvatarChange = { avatar = it },
                            onColorChange = { color = it },
                        )
                    }
                }

                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Box {
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(PebbleDimens.rowShape)
                                    .clickable { harnessMenuOpen = true }
                                    .padding(horizontal = 4.dp, vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text(store.t("harnessBackend"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(
                                    text = harnesses.firstOrNull { it.id == harness }?.title ?: harness,
                                    color = p.text,
                                    fontSize = PebbleFont.body,
                                    fontWeight = FontWeight.Medium,
                                )
                                Icon(Icons.Rounded.ArrowDropDown, contentDescription = null, tint = p.mute)
                            }
                            DropdownMenu(
                                expanded = harnessMenuOpen,
                                onDismissRequest = { harnessMenuOpen = false },
                                containerColor = p.card,
                            ) {
                                harnesses.forEach { h ->
                                    DropdownMenuItem(
                                        text = { Text(h.title, color = p.text) },
                                        onClick = {
                                            harness = h.id
                                            harnessTouched = true
                                            harnessMenuOpen = false
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
                                value = model,
                                onValueChange = { model = it },
                                label = { Text(store.t("modelOptional"), fontSize = PebbleFont.secondary) },
                                placeholder = { Text(ph.ifEmpty { store.t("inheritDefaultPh") }, fontFamily = MonoFont) },
                                singleLine = true,
                                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                                shape = PebbleDimens.rowShape,
                                colors = pebbleFieldColors(),
                                modifier = Modifier.weight(1f),
                            )
                            Box {
                                IconButton(onClick = { modelMenuOpen = true }) {
                                    Icon(Icons.AutoMirrored.Rounded.List, contentDescription = store.t("optionalModels"), tint = p.accent)
                                }
                                DropdownMenu(
                                    expanded = modelMenuOpen,
                                    onDismissRequest = { modelMenuOpen = false },
                                    containerColor = p.card,
                                ) {
                                    if (model.trim().isNotEmpty()) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("clear"), color = p.danger) },
                                            onClick = {
                                                model = ""
                                                modelMenuOpen = false
                                            },
                                        )
                                        HorizontalDivider(color = p.s2)
                                    }
                                    if (cands.isEmpty()) {
                                        DropdownMenuItem(
                                            text = { Text(if (loadingModels) store.t("loading") else store.t("optionalModels"), color = p.text) },
                                            enabled = !loadingModels,
                                            onClick = { loadCands(harness) },
                                        )
                                    } else {
                                        cands.take(40).forEach { m ->
                                            DropdownMenuItem(
                                                text = { Text(m, fontFamily = MonoFont, fontSize = PebbleFont.secondary, color = p.text) },
                                                onClick = {
                                                    model = m
                                                    modelMenuOpen = false
                                                },
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                SectionHeader(store.t("agentsCreateLbl"))
                SectionCard {
                    OutlinedTextField(
                        value = instructions,
                        onValueChange = { instructions = it },
                        placeholder = { Text(store.t("agentsCreatePh")) },
                        minLines = 4,
                        maxLines = 12,
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(14.dp),
                    )
                }

                if (err.isNotEmpty()) {
                    SectionCard {
                        Box(modifier = Modifier.padding(14.dp)) {
                            StatusTextBanner(text = err, error = true)
                        }
                    }
                }

                Spacer(Modifier.height(16.dp))
            }
        }
    }
}
