package ai.pebble.android.ui.chat

import androidx.compose.foundation.layout.heightIn
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.sp
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import ai.pebble.android.i18n.L
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.PebblePalette
import ai.pebble.android.ui.theme.copyToClipboard
import coil3.compose.SubcomposeAsyncImage
import kotlinx.coroutines.delay

// MARK: - Compose Views

@Composable
fun MarkdownText(
    text: String,
    modifier: Modifier = Modifier,
    color: Color? = null,
) {
    val p = LocalPalette.current
    val fg = color ?: p.text
    val blocks = remember(text, L.lang) { MarkdownParser.parse(text) }

    SelectionContainer(modifier = modifier) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            blocks.forEach { block ->
                RenderMarkdownBlock(block = block, p = p, fg = fg)
            }
        }
    }
}

@Composable
private fun RenderMarkdownBlock(
    block: MarkdownBlock,
    p: PebblePalette,
    fg: Color,
) {
    when (block) {
        is MarkdownBlock.Code -> CodeCard(lang = block.lang, code = block.body)
        is MarkdownBlock.Math -> MathBlockView(expr = block.expr, color = fg)
        is MarkdownBlock.Details -> DetailsBlockView(summary = block.summary, initiallyOpen = block.isOpen) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                block.body.forEach { sub -> RenderMarkdownBlock(sub, p, fg) }
            }
        }
        is MarkdownBlock.Heading -> {
            val size = when (block.level) {
                1 -> PebbleFont.hero
                2 -> PebbleFont.title
                3 -> PebbleFont.cardTitle
                else -> PebbleFont.body
            }
            val headColor = if (block.level >= 5) p.text2 else fg
            val inlineText = remember(block.text, p.dark, headColor, L.lang) { buildInlineMarkdown(block.text, p, headColor) }
            Text(
                text = inlineText,
                fontSize = size,
                fontWeight = FontWeight.Bold,
                color = headColor,
            )
        }
        is MarkdownBlock.Rule -> {
            HorizontalDivider(
                modifier = Modifier.padding(vertical = 4.dp),
                thickness = 1.dp,
                color = p.s3,
            )
        }
        is MarkdownBlock.Quote -> {
            val quoteText = remember(block.text, p.dark, L.lang) { buildInlineMarkdown(block.text, p, p.text2) }
            Row(
                modifier = Modifier.height(IntrinsicSize.Min),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                repeat(block.level.coerceIn(1, 4)) {
                    Box(
                        modifier = Modifier
                            .width(3.dp)
                            .fillMaxHeight()
                            .clip(RoundedCornerShape(2.dp))
                            .background(p.s3),
                    )
                }
                Text(
                    text = quoteText,
                    fontSize = PebbleFont.body,
                    lineHeight = 22.sp,
                    color = p.text2,
                )
            }
        }
        is MarkdownBlock.Table -> TableBlockView(headers = block.headers, aligns = block.aligns, rows = block.rows, fg = fg)
        is MarkdownBlock.ListBlock -> {
            Column(verticalArrangement = Arrangement.spacedBy(5.dp)) {
                block.items.forEach { item ->
                    Row(
                        modifier = Modifier.padding(start = (item.indent * 16).dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        if (item.task) {
                            Icon(
                                painter = painterResource(if (item.checked) PebbleIcons.CheckBox else PebbleIcons.CheckBoxOutlineBlank),
                                contentDescription = null,
                                tint = if (item.checked) p.accent else p.mute,
                                modifier = Modifier
                                    .padding(top = 3.dp)
                                    .size(16.dp),
                            )
                        } else {
                            Text(
                                text = item.marker,
                                fontSize = PebbleFont.body,
                                lineHeight = 22.sp,
                                color = p.mute,
                            )
                        }
                        val itemColor = if (item.task && item.checked) p.text2 else fg
                        val itemText = remember(item.text, p.dark, itemColor, L.lang) { buildInlineMarkdown(item.text, p, itemColor) }
                        Text(
                            text = itemText,
                            fontSize = PebbleFont.body,
                            lineHeight = 22.sp,
                            color = itemColor,
                            textDecoration = if (item.task && item.checked) TextDecoration.LineThrough else null,
                        )
                    }
                }
            }
        }
        is MarkdownBlock.Footnote -> {
            val fnText = remember(block.text, p.dark, L.lang) { buildInlineMarkdown(block.text, p, p.text2) }
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    text = "[${block.id}]",
                    fontSize = PebbleFont.caption,
                    fontWeight = FontWeight.SemiBold,
                    color = p.mute,
                )
                Text(
                    text = fnText,
                    fontSize = PebbleFont.secondary,
                    lineHeight = 19.sp,
                    color = p.text2,
                )
            }
        }
        is MarkdownBlock.ImageBlock -> MarkdownImageView(alt = block.alt, url = block.url)
        is MarkdownBlock.Para -> {
            val paraText = remember(block.text, p.dark, fg, L.lang) { buildInlineMarkdown(block.text, p, fg) }
            Text(
                text = paraText,
                fontSize = PebbleFont.body,
                lineHeight = 22.sp,
                color = fg,
            )
        }
    }
}

@Composable
private fun DetailsBlockView(
    summary: String,
    initiallyOpen: Boolean,
    content: @Composable () -> Unit,
) {
    val p = LocalPalette.current
    var expanded by remember(summary, initiallyOpen) { mutableStateOf(initiallyOpen) }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(p.s1)
            .padding(horizontal = 12.dp, vertical = 9.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded },
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(
                painter = painterResource(if (expanded) PebbleIcons.KeyboardArrowDown else PebbleIcons.ChevronRight),
                contentDescription = null,
                tint = p.mute,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = summary,
                color = p.text,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
            )
        }
        AnimatedVisibility(visible = expanded) {
            Column {
                HorizontalDivider(
                    modifier = Modifier.padding(vertical = 8.dp),
                    thickness = 1.dp,
                    color = p.s2,
                )
                content()
            }
        }
    }
}

@Composable
private fun TableBlockView(
    headers: List<String>,
    aligns: List<TableAlign>,
    rows: List<List<String>>,
    fg: Color,
) {
    val p = LocalPalette.current
    val colCount = maxOf(headers.size, aligns.size, rows.maxOfOrNull { it.size } ?: 0).coerceAtLeast(1)
    val allRows = listOf(headers) + rows

    Box(
        modifier = Modifier
            .horizontalScroll(rememberScrollState())
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, p.s2, RoundedCornerShape(12.dp)),
    ) {
        Layout(
            content = {
                allRows.forEachIndexed { rIdx, row ->
                    val isHeader = rIdx == 0
                    for (col in 0 until colCount) {
                        val align = aligns.getOrElse(col) { TableAlign.LEFT }
                        val cellText = row.getOrElse(col) { "" }
                        Box(
                            modifier = Modifier
                                .background(if (isHeader) p.s1 else Color.Transparent)
                                .then(
                                    if (col < colCount - 1) {
                                        Modifier.drawBehind {
                                            val stroke = 1.dp.toPx()
                                            val x = size.width - stroke / 2f
                                            drawLine(p.s2, Offset(x, 0f), Offset(x, size.height), strokeWidth = stroke)
                                        }
                                    } else Modifier
                                )
                                .padding(horizontal = 10.dp, vertical = 7.dp),
                            contentAlignment = align.boxAlign(),
                        ) {
                            val cellInline = remember(cellText, p.dark, fg, L.lang) { buildInlineMarkdown(cellText, p, fg) }
                            Text(
                                text = cellInline,
                                fontSize = PebbleFont.secondary,
                                fontWeight = if (isHeader) FontWeight.SemiBold else FontWeight.Normal,
                                color = fg,
                                textAlign = align.textAlign(),
                                modifier = Modifier.widthIn(max = 260.dp),
                            )
                        }
                    }
                    if (rIdx < allRows.lastIndex) {
                        Box(modifier = Modifier.height(1.dp).background(p.s2))
                    }
                }
            },
        ) { measurables, _ ->
            val minCellPx = 48.dp.roundToPx()
            val maxCellPx = 260.dp.roundToPx()
            val minRowPx = 24.dp.roundToPx()
            val divH = 1.dp.roundToPx().coerceAtLeast(1)
            val rowCount = allRows.size

            // Pass 1 (intrinsic only): compute unified width per column
            val colWidths = IntArray(colCount) { minCellPx }
            var idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    val w = measurables[idx++].maxIntrinsicWidth(Int.MAX_VALUE).coerceIn(minCellPx, maxCellPx)
                    if (w > colWidths[c]) colWidths[c] = w
                }
                if (r < rowCount - 1) idx++
            }

            // Pass 2 (intrinsic only): compute unified height per row
            val rowHeights = IntArray(rowCount) { minRowPx }
            idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    val h = measurables[idx++].minIntrinsicHeight(colWidths[c]).coerceAtLeast(minRowPx)
                    if (h > rowHeights[r]) rowHeights[r] = h
                }
                if (r < rowCount - 1) idx++
            }

            // Pass 3 (single measure per measurable)
            val totalW = colWidths.sum()
            val cellPlaceables = Array(rowCount) { arrayOfNulls<androidx.compose.ui.layout.Placeable>(colCount) }
            val dividerPlaceables = mutableListOf<androidx.compose.ui.layout.Placeable>()
            idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    cellPlaceables[r][c] = measurables[idx++].measure(
                        Constraints.fixed(colWidths[c], rowHeights[r])
                    )
                }
                if (r < rowCount - 1) {
                    dividerPlaceables.add(measurables[idx++].measure(Constraints.fixed(totalW, divH)))
                }
            }

            val finalDividers = dividerPlaceables
            val totalH = rowHeights.sum() + divH * (rowCount - 1).coerceAtLeast(0)

            layout(totalW, totalH) {
                var y = 0
                for (r in 0 until rowCount) {
                    var x = 0
                    for (c in 0 until colCount) {
                        cellPlaceables[r][c]?.placeRelative(x, y)
                        x += colWidths[c]
                    }
                    y += rowHeights[r]
                    if (r < rowCount - 1) {
                        finalDividers[r].placeRelative(0, y)
                        y += divH
                    }
                }
            }
        }
    }
}

@Composable
private fun MarkdownImageView(alt: String, url: String) {
    val p = LocalPalette.current
    val uriHandler = LocalUriHandler.current
    val label = if (alt.isEmpty()) L.t("mdImageFallback") else L.t("mdImageFallbackAlt", mapOf("alt" to alt))

    if (isSafeUrl(url)) {
        SubcomposeAsyncImage(
            model = url,
            contentDescription = alt.ifEmpty { null },
            contentScale = ContentScale.Fit,
            modifier = Modifier
                .widthIn(max = 320.dp)
                .heightIn(max = 280.dp)
                .clip(RoundedCornerShape(12.dp)),
            loading = {
                Box(
                    modifier = Modifier
                        .size(width = 180.dp, height = 110.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(p.s1),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.mute)
                }
            },
            error = {
                Text(
                    text = label,
                    color = p.accent,
                    fontSize = PebbleFont.secondary,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(p.s1)
                        .clickable { runCatching { uriHandler.openUri(url) } }
                        .padding(horizontal = 10.dp, vertical = 5.dp),
                )
            },
        )
    } else {
        Text(
            text = label,
            color = p.text2,
            fontSize = PebbleFont.secondary,
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(p.s1)
                .padding(horizontal = 10.dp, vertical = 5.dp),
        )
    }
}

@Composable
fun CodeCard(
    lang: String,
    code: String,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    var copied by remember { mutableStateOf(false) }

    LaunchedEffect(copied) {
        if (copied) {
            delay(1500)
            copied = false
        }
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(p.s2.copy(alpha = if (p.dark) 0.6f else 0.7f)),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = lang.ifEmpty { "code" },
                color = p.mute,
                fontSize = PebbleFont.secondary,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (copied) L.t("copied") else L.t("copy"),
                color = p.text2,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .clickable {
                        copyToClipboard(context, code)
                        copied = true
                    }
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
        }
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(start = 12.dp, end = 12.dp, bottom = 12.dp),
        ) {
            Text(
                text = code,
                fontFamily = MonoFont,
                fontSize = PebbleFont.secondary,
                lineHeight = 20.sp,
                color = p.text,
            )
        }
    }
}
