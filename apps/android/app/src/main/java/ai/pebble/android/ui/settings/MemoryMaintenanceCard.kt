package ai.pebble.android.ui.settings

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryMagpieStatus
import ai.pebble.android.data.MemorySemanticStatus
import ai.pebble.android.data.MemoryStatusResp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun MemoryMaintenanceCard(
    store: PebbleStore,
    status: MemoryStatusResp?,
    magpie: MemoryMagpieStatus?,
    semantic: MemorySemanticStatus?,
    coexistence: String,
    dreamEnabled: Boolean,
    dreamDailyAt: String,
    dreamModel: String,
    embeddingEnabled: Boolean,
    embeddingBaseUrl: String,
    embeddingModel: String,
    embeddingApiKey: String,
    hasSavedEmbeddingApiKey: Boolean,
    configDirty: Boolean,
    busy: String,
    onCoexistenceChange: (String) -> Unit,
    onDreamEnabledChange: (Boolean) -> Unit,
    onDreamDailyAtChange: (String) -> Unit,
    onDreamModelChange: (String) -> Unit,
    onEmbeddingEnabledChange: (Boolean) -> Unit,
    onEmbeddingBaseUrlChange: (String) -> Unit,
    onEmbeddingModelChange: (String) -> Unit,
    onEmbeddingApiKeyChange: (String) -> Unit,
    onSaveConfig: () -> Unit,
    onRunDream: () -> Unit,
    onReindex: () -> Unit,
    onImportMagpie: (String) -> Unit,
) {
    val p = LocalPalette.current
    var embeddingOpen by remember { mutableStateOf(false) }
    var dreamsOpen by remember { mutableStateOf(false) }
    var showApiKey by remember { mutableStateOf(false) }

    SectionHeader(store.t("memSettingsTitle"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // Native status overview
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                TagPill(
                    text = store.t("memBackendNative"),
                    bg = p.sage,
                    fg = p.sageInk,
                )
                val semOn = semantic?.active == true
                TagPill(
                    text = store.t(if (semOn) "memSemanticOn" else "memFtsOnly"),
                    bg = if (semOn) p.sage else p.s1,
                    fg = if (semOn) p.sageInk else p.text2,
                )
            }
            if (status != null) {
                Text(
                    text = store.t(
                        "memStatsSummary",
                        mapOf("docs" to status.docCount, "chunks" to status.chunkCount),
                    ),
                    color = p.mute,
                    fontSize = PebbleFont.caption,
                )
            }

            // Coexistence mode
            Text(
                text = store.t("memCoexistence"),
                color = p.text,
                fontSize = PebbleFont.body,
                fontWeight = FontWeight.Medium,
            )
            val modes = listOf(
                "auto" to store.t("memCoexistAuto"),
                "isolated" to store.t("memCoexistIsolated"),
            )
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                modes.forEachIndexed { idx, (mode, label) ->
                    SegmentedButton(
                        selected = coexistence == mode,
                        onClick = { onCoexistenceChange(mode) },
                        shape = SegmentedButtonDefaults.itemShape(idx, modes.size),
                        label = {
                            Text(
                                text = label,
                                fontSize = PebbleFont.secondary,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        },
                    )
                }
            }
            Text(store.t("memCoexistHint"), color = p.mute, fontSize = PebbleFont.caption)
            val injected = magpie?.injectedHarnesses.orEmpty()
            if (injected.isNotEmpty()) {
                Text(
                    text = store.t("memMagpieHarnesses", mapOf("list" to injected.joinToString(", "))),
                    color = p.text2,
                    fontSize = PebbleFont.caption,
                    fontFamily = MonoFont,
                )
            }

            CardDivider()

            // Dream Consolidation
            Text(
                text = store.t("memDreamSection"),
                color = p.text,
                fontSize = PebbleFont.body,
                fontWeight = FontWeight.Medium,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = store.t("memDreamEnabled"),
                    color = p.text,
                    fontSize = PebbleFont.secondary,
                    modifier = Modifier.weight(1f),
                )
                PebbleSwitch(checked = dreamEnabled, onCheckedChange = onDreamEnabledChange)
            }
            Text(store.t("memDreamDailyAt"), color = p.text2, fontSize = PebbleFont.caption)
            OutlinedTextField(
                value = dreamDailyAt,
                onValueChange = onDreamDailyAtChange,
                placeholder = { Text(store.t("memDreamDailyAtPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            Text(store.t("memDreamModel"), color = p.text2, fontSize = PebbleFont.caption)
            OutlinedTextField(
                value = dreamModel,
                onValueChange = onDreamModelChange,
                placeholder = { Text(store.t("memDreamModelPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )

            // Maintenance actions (Run Dream / Reindex)
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                OutlinedButton(
                    onClick = onRunDream,
                    enabled = busy.isEmpty(),
                ) {
                    if (busy == "mem:dream") {
                        CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.accent)
                        Spacer(Modifier.width(6.dp))
                        Text(store.t("memDreamRunning"), color = p.text, fontSize = PebbleFont.secondary)
                    } else {
                        Text(store.t("memRunDream"), color = p.text, fontSize = PebbleFont.secondary)
                    }
                }
                OutlinedButton(
                    onClick = onReindex,
                    enabled = busy.isEmpty(),
                ) {
                    if (busy == "mem:reindex") {
                        CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.accent)
                        Spacer(Modifier.width(6.dp))
                        Text(store.t("memReindexing"), color = p.text, fontSize = PebbleFont.secondary)
                    } else {
                        Text(store.t("memReindex"), color = p.text, fontSize = PebbleFont.secondary)
                    }
                }
            }

            // Recent dreams (if available)
            val recentDreams = status?.recentDreams.orEmpty()
            if (recentDreams.isNotEmpty()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { dreamsOpen = !dreamsOpen }
                        .padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "${store.t("memRecentDreams")} (${recentDreams.size})",
                        color = p.text2,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(
                        painter = painterResource(if (dreamsOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                        contentDescription = null,
                        tint = p.mute,
                    )
                }
                AnimatedVisibility(visible = dreamsOpen) {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        recentDreams.take(6).forEach { entry ->
                            Column(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(PebbleDimens.rowShape)
                                    .background(p.s1)
                                    .padding(10.dp),
                                verticalArrangement = Arrangement.spacedBy(2.dp),
                            ) {
                                val head = listOfNotNull(
                                    entry.date?.takeIf { it.isNotBlank() },
                                    entry.botId?.takeIf { it.isNotBlank() }?.let { "@$it" },
                                ).joinToString(" · ")
                                if (head.isNotEmpty()) {
                                    Text(head, color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                                }
                                if (!entry.summary.isNullOrBlank()) {
                                    Text(
                                        text = entry.summary,
                                        color = p.text,
                                        fontSize = PebbleFont.secondary,
                                        maxLines = 3,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                        }
                    }
                }
            }

            CardDivider()

            // Optional Semantic Embedding settings
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { embeddingOpen = !embeddingOpen }
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = store.t("memEmbeddingSection"),
                    color = p.text,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    painter = painterResource(if (embeddingOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                    contentDescription = null,
                    tint = p.mute,
                )
            }
            AnimatedVisibility(visible = embeddingOpen) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = store.t("memEmbeddingEnabled"),
                            color = p.text,
                            fontSize = PebbleFont.secondary,
                            modifier = Modifier.weight(1f),
                        )
                        PebbleSwitch(checked = embeddingEnabled, onCheckedChange = onEmbeddingEnabledChange)
                    }
                    Text(store.t("memEmbeddingBaseUrl"), color = p.text2, fontSize = PebbleFont.caption)
                    OutlinedTextField(
                        value = embeddingBaseUrl,
                        onValueChange = onEmbeddingBaseUrlChange,
                        placeholder = { Text(store.t("memEmbeddingBaseUrlPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                        singleLine = true,
                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Text(store.t("memEmbeddingModel"), color = p.text2, fontSize = PebbleFont.caption)
                    OutlinedTextField(
                        value = embeddingModel,
                        onValueChange = onEmbeddingModelChange,
                        placeholder = { Text(store.t("memEmbeddingModelPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                        singleLine = true,
                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Text(store.t("memEmbeddingApiKey"), color = p.text2, fontSize = PebbleFont.caption)
                    OutlinedTextField(
                        value = embeddingApiKey,
                        onValueChange = onEmbeddingApiKeyChange,
                        placeholder = {
                            Text(
                                store.t(if (hasSavedEmbeddingApiKey) "memEmbeddingApiKeySetPh" else "memEmbeddingApiKeyPh"),
                                fontSize = PebbleFont.secondary,
                            )
                        },
                        singleLine = true,
                        visualTransformation = if (showApiKey) VisualTransformation.None else PasswordVisualTransformation(),
                        trailingIcon = {
                            IconButton(onClick = { showApiKey = !showApiKey }) {
                                Icon(
                                    painter = painterResource(if (showApiKey) PebbleIcons.VisibilityOff else PebbleIcons.Visibility),
                                    contentDescription = store.t(if (showApiKey) "hide" else "show"),
                                    tint = p.mute,
                                )
                            }
                        },
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                Button(
                    onClick = onSaveConfig,
                    enabled = configDirty && busy.isEmpty(),
                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                ) {
                    if (busy == "mem:config") {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.onAccent)
                    } else {
                        Text(store.t("save"), fontSize = PebbleFont.secondary)
                    }
                }
            }

            // Non-blocking Magpie import shown ONLY when magpie.installed == true
            if (magpie?.installed == true) {
                CardDivider()
                Text(
                    text = store.t("memMagpieImportTitle"),
                    color = p.text,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                )
                val legacyCount = magpie.legacyCount ?: 0
                if (legacyCount > 0) {
                    Text(
                        text = store.t("memMagpieLegacyHint", mapOf("n" to legacyCount)),
                        color = p.text2,
                        fontSize = PebbleFont.secondary,
                    )
                }
                Text(
                    text = store.t("memMagpieImportHint"),
                    color = p.mute,
                    fontSize = PebbleFont.caption,
                )
                FlowRow(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    OutlinedButton(
                        onClick = { onImportMagpie("bots_only") },
                        enabled = busy.isEmpty(),
                    ) {
                        if (busy == "mem:import:bots_only") {
                            CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.accent)
                            Spacer(Modifier.width(6.dp))
                            Text(store.t("memImporting"), color = p.text, fontSize = PebbleFont.secondary)
                        } else {
                            Text(store.t("memImportBotsOnly"), color = p.text, fontSize = PebbleFont.secondary)
                        }
                    }
                    OutlinedButton(
                        onClick = { onImportMagpie("all") },
                        enabled = busy.isEmpty(),
                    ) {
                        if (busy == "mem:import:all") {
                            CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.accent)
                            Spacer(Modifier.width(6.dp))
                            Text(store.t("memImporting"), color = p.text, fontSize = PebbleFont.secondary)
                        } else {
                            Text(store.t("memImportAll"), color = p.text, fontSize = PebbleFont.secondary)
                        }
                    }
                }
            }
        }
    }
}
