package ai.pebble.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.PendingQuestion
import ai.pebble.android.data.QuestionField
import ai.pebble.android.data.asBoolValue
import ai.pebble.android.data.asDisplayString
import ai.pebble.android.data.asStringList
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

@Composable
fun QuestionCardView(
    store: PebbleStore,
    question: PendingQuestion,
    pal: BotAccent,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    val singles = remember(question.id) { mutableStateMapOf<String, String>() }
    val multis = remember(question.id) { mutableStateMapOf<String, List<String>>() }
    val bools = remember(question.id) { mutableStateMapOf<String, Boolean>() }
    val customText = remember(question.id) { mutableStateMapOf<String, String>() }
    val customActive = remember(question.id) { mutableStateMapOf<String, Boolean>() }
    var submitting by remember(question.id) { mutableStateOf(false) }

    LaunchedEffect(question.id) {
        singles.clear()
        multis.clear()
        bools.clear()
        customText.clear()
        customActive.clear()
        for (f in question.fields) {
            when (f.type) {
                "multiselect" -> multis[f.key] = f.defaultValue.asStringList().orEmpty()
                "boolean" -> f.defaultValue.asBoolValue()?.let { bools[f.key] = it }
                else -> f.defaultValue.asDisplayString()?.let { singles[f.key] = it }
            }
        }
        submitting = false
    }

    val canSubmit = question.fields.all { f ->
        if (f.required != true) return@all true
        when {
            f.type == "multiselect" -> {
                val c = customText[f.key].orEmpty().trim()
                multis[f.key].orEmpty().isNotEmpty() || (f.custom == true && c.isNotEmpty())
            }
            f.type == "boolean" -> bools[f.key] != null
            !f.options.isNullOrEmpty() && customActive[f.key] == true ->
                customText[f.key].orEmpty().trim().isNotEmpty()
            else -> singles[f.key].orEmpty().trim().isNotEmpty()
        }
    }

    fun submit(cancelled: Boolean) {
        if (submitting) return
        submitting = true
        val answers = mutableMapOf<String, Any?>()
        if (!cancelled) {
            for (f in question.fields) {
                when {
                    f.type == "multiselect" -> {
                        val list = multis[f.key].orEmpty().toMutableList()
                        val c = customText[f.key].orEmpty().trim()
                        if (f.custom == true && c.isNotEmpty() && c !in list) list.add(c)
                        answers[f.key] = list
                    }
                    f.type == "boolean" -> answers[f.key] = bools[f.key] ?: false
                    f.type == "integer" -> {
                        singles[f.key].orEmpty().trim().toIntOrNull()?.let { answers[f.key] = it }
                    }
                    f.type == "number" -> {
                        singles[f.key].orEmpty().trim().toDoubleOrNull()?.let { answers[f.key] = it }
                    }
                    !f.options.isNullOrEmpty() && customActive[f.key] == true -> {
                        answers[f.key] = customText[f.key].orEmpty().trim()
                    }
                    else -> answers[f.key] = singles[f.key].orEmpty()
                }
            }
        }
        scope.launch {
            runCatching { store.replyQuestion(question.id, answers, cancelled) }
            submitting = false
        }
    }

    Surface(
        modifier = modifier.fillMaxWidth(),
        color = p.card,
        shape = RoundedCornerShape(20.dp),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = question.title?.takeIf { it.isNotEmpty() } ?: store.t("questionTitle"),
                    color = p.text,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.weight(1f),
                )
                TagPill(
                    text = store.t("questionWaitingBadge"),
                    bg = p.s1,
                    fg = p.busy,
                )
            }

            question.fields.forEach { f ->
                QuestionFieldItem(
                    store = store,
                    field = f,
                    pal = pal,
                    submitting = submitting,
                    singleValue = singles[f.key].orEmpty(),
                    onSingleChange = { singles[f.key] = it },
                    multiValues = multis[f.key].orEmpty(),
                    onMultiChange = { multis[f.key] = it },
                    boolValue = bools[f.key],
                    onBoolChange = { bools[f.key] = it },
                    customText = customText[f.key].orEmpty(),
                    onCustomTextChange = { customText[f.key] = it },
                    customActive = customActive[f.key] == true,
                    onCustomActiveChange = { customActive[f.key] = it },
                )
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
            ) {
                OutlinedButton(
                    onClick = { submit(cancelled = true) },
                    enabled = !submitting,
                ) {
                    Text(store.t("questionCancel"), color = p.text)
                }
                Button(
                    onClick = { submit(cancelled = false) },
                    enabled = !submitting && canSubmit,
                    colors = ButtonDefaults.buttonColors(
                        containerColor = pal.accent,
                        contentColor = pal.onAccent,
                    ),
                ) {
                    Text(store.t("questionSubmit"))
                }
            }
        }
    }
}

@Composable
private fun QuestionFieldItem(
    store: PebbleStore,
    field: QuestionField,
    pal: BotAccent,
    submitting: Boolean,
    singleValue: String,
    onSingleChange: (String) -> Unit,
    multiValues: List<String>,
    onMultiChange: (List<String>) -> Unit,
    boolValue: Boolean?,
    onBoolChange: (Boolean) -> Unit,
    customText: String,
    onCustomTextChange: (String) -> Unit,
    customActive: Boolean,
    onCustomActiveChange: (Boolean) -> Unit,
) {
    val p = LocalPalette.current
    val isMulti = field.type == "multiselect"
    val opts = field.options.orEmpty()

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (!field.title.isNullOrEmpty() || isMulti) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (!field.title.isNullOrEmpty()) {
                    Text(
                        text = field.title,
                        color = p.text,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
                Spacer(Modifier.weight(1f))
                if (isMulti) {
                    Text(store.t("questionMultiHint"), color = p.mute, fontSize = 12.sp)
                }
            }
        }
        if (!field.description.isNullOrEmpty()) {
            Text(field.description, color = p.text2, fontSize = 14.sp)
        }

        when {
            field.type == "boolean" -> {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    listOf(true to store.t("questionYes"), false to store.t("questionNo")).forEach { (v, label) ->
                        val active = boolValue == v
                        Column(
                            modifier = Modifier
                                .weight(1f)
                                .clip(PebbleDimens.rowShape)
                                .background(if (active) pal.soft else p.s1)
                                .border(
                                    width = if (active) 1.dp else 0.dp,
                                    color = if (active) pal.accent else Color.Transparent,
                                    shape = PebbleDimens.rowShape,
                                )
                                .clickable(enabled = !submitting) { onBoolChange(v) }
                                .padding(vertical = 10.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Text(label, color = p.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
                        }
                    }
                }
            }
            opts.isNotEmpty() -> {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    opts.forEach { opt ->
                        val active = if (isMulti) {
                            opt.value in multiValues
                        } else {
                            !customActive && singleValue == opt.value
                        }
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .background(if (active) pal.soft else p.s1)
                                .border(
                                    width = if (active) 1.dp else 0.dp,
                                    color = if (active) pal.accent else Color.Transparent,
                                    shape = PebbleDimens.rowShape,
                                )
                                .clickable(enabled = !submitting) {
                                    if (isMulti) {
                                        val next = multiValues.toMutableList()
                                        if (opt.value in next) next.remove(opt.value) else next.add(opt.value)
                                        onMultiChange(next)
                                    } else {
                                        onCustomActiveChange(false)
                                        onSingleChange(opt.value)
                                    }
                                }
                                .padding(horizontal = 12.dp, vertical = 10.dp),
                            verticalArrangement = Arrangement.spacedBy(2.dp),
                        ) {
                            Text(
                                text = opt.label?.takeIf { it.isNotEmpty() } ?: opt.value,
                                color = p.text,
                                fontSize = 15.sp,
                                fontWeight = FontWeight.SemiBold,
                            )
                            if (!opt.description.isNullOrEmpty()) {
                                Text(opt.description, color = p.text2, fontSize = 12.sp)
                            }
                        }
                    }
                    if (field.custom == true) {
                        OutlinedTextField(
                            value = customText,
                            onValueChange = { v ->
                                onCustomTextChange(v)
                                if (!isMulti) onCustomActiveChange(v.isNotEmpty())
                            },
                            placeholder = { Text(store.t("questionCustomPlaceholder")) },
                            enabled = !submitting,
                            shape = PebbleDimens.rowShape,
                            colors = pebbleFieldColors(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }
            else -> {
                OutlinedTextField(
                    value = singleValue,
                    onValueChange = onSingleChange,
                    placeholder = { Text(field.placeholder ?: store.t("questionInputPlaceholder")) },
                    enabled = !submitting,
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}
