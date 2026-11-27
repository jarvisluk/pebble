package ai.pebble.android.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.pebbleFieldColors

data class MemoryFormInput(
    val title: String,
    val kind: String,
    val text: String,
    val tags: List<String>,
    val why: String,
    val pin: Boolean,
    val daily: Boolean,
)

@Composable
fun MemoryEditorForm(
    store: PebbleStore,
    textPlaceholder: String,
    accent: Color,
    onAccent: Color,
    saving: Boolean,
    initial: MemoryItem? = null,
    allowDaily: Boolean = false,
    onCancel: (() -> Unit)? = null,
    onSubmit: (MemoryFormInput) -> Unit,
) {
    val p = LocalPalette.current
    val isEdit = initial != null

    var title by remember(initial?.id) { mutableStateOf(initial?.title.orEmpty()) }
    var kind by remember(initial?.id) { mutableStateOf(initial?.kind?.ifBlank { "fact" } ?: "fact") }
    var text by remember(initial?.id) {
        mutableStateOf(initial?.text?.takeIf { it.isNotBlank() } ?: initial?.snippet.orEmpty())
    }
    var tagsRaw by remember(initial?.id) {
        mutableStateOf(initial?.tags.orEmpty().filterNot { it.startsWith("bot:") }.joinToString(", "))
    }
    var why by remember(initial?.id) { mutableStateOf(initial?.why.orEmpty()) }
    var pin by remember(initial?.id) { mutableStateOf(initial?.pin == true) }
    var daily by remember(initial?.id) { mutableStateOf(false) }
    var kindOpen by remember { mutableStateOf(false) }

    val containerMod = if (isEdit) {
        Modifier
            .fillMaxWidth()
            .clip(PebbleDimens.rowShape)
            .background(p.s1)
            .padding(12.dp)
    } else {
        Modifier.fillMaxWidth()
    }

    Column(modifier = containerMod, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (isEdit) {
            Text(
                text = store.t("memEditTitle"),
                color = p.text,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
            )
        }

        if (allowDaily && !isEdit) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = store.t("memDailyLabel"),
                    color = p.text,
                    fontSize = PebbleFont.secondary,
                    modifier = Modifier.weight(1f),
                )
                PebbleSwitch(checked = daily, onCheckedChange = { daily = it })
            }
        }

        if (!daily) {
            Box {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { kindOpen = true }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(store.t("memType"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                    Text(
                        text = localizedMemoryKind(kind, store),
                        color = p.text,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                    )
                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                }
                DropdownMenu(
                    expanded = kindOpen,
                    onDismissRequest = { kindOpen = false },
                    containerColor = p.card,
                ) {
                    MEMORY_KINDS.forEach { k ->
                        DropdownMenuItem(
                            text = { Text(localizedMemoryKind(k, store), color = p.text, fontSize = PebbleFont.body) },
                            trailingIcon = if (kind == k) {
                                { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = accent, modifier = Modifier.size(16.dp)) }
                            } else {
                                null
                            },
                            onClick = { kind = k; kindOpen = false },
                        )
                    }
                }
            }

            OutlinedTextField(
                value = title,
                onValueChange = { title = it },
                placeholder = { Text(store.t("memTitlePh"), fontSize = PebbleFont.secondary) },
                singleLine = true,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
        }

        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            placeholder = { Text(textPlaceholder, fontSize = PebbleFont.secondary) },
            minLines = 2,
            maxLines = 6,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        if (!daily) {
            OutlinedTextField(
                value = tagsRaw,
                onValueChange = { tagsRaw = it },
                placeholder = { Text(store.t("tagsPh"), fontSize = PebbleFont.secondary) },
                singleLine = true,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = why,
                onValueChange = { why = it },
                placeholder = { Text(store.t("memWhyPh"), fontSize = PebbleFont.secondary) },
                minLines = 1,
                maxLines = 3,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = store.t("memPinLabel"),
                    color = p.text,
                    fontSize = PebbleFont.secondary,
                    modifier = Modifier.weight(1f),
                )
                PebbleSwitch(checked = pin, onCheckedChange = { pin = it })
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.End,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (onCancel != null) {
                OutlinedButton(onClick = onCancel, enabled = !saving) {
                    Text(store.t("cancel"), color = p.text, fontSize = PebbleFont.secondary)
                }
                Spacer(Modifier.width(8.dp))
            }
            Button(
                onClick = {
                    onSubmit(
                        MemoryFormInput(
                            title = title.trim(),
                            kind = kind,
                            text = text.trim(),
                            tags = parseMemoryTags(tagsRaw),
                            why = why.trim(),
                            pin = pin,
                            daily = daily,
                        ),
                    )
                },
                enabled = !saving && text.trim().isNotEmpty(),
                colors = ButtonDefaults.buttonColors(containerColor = accent, contentColor = onAccent),
            ) {
                if (saving) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = onAccent)
                } else {
                    Text(store.t(if (isEdit) "save" else "saveMemory"), fontSize = PebbleFont.secondary)
                }
            }
        }
    }
}
