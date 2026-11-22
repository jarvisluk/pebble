import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import React, { useState } from 'react';
import { Linking, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { useI18n } from '../i18n';
import { MONO_FONT, type Theme } from '../theme';

function CodeCard({ lang, code, theme }: { lang: string; code: string; theme: Theme }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    await Clipboard.setStringAsync(code);
    if (Platform.OS !== 'web') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };

  return (
    <View style={[styles.codeWrap, { backgroundColor: theme.s1 }]}>
      <View style={[styles.codeHead, { borderBottomColor: theme.s2 }]}>
        <Text style={[styles.codeLang, { color: theme.mute }]}>{lang || 'code'}</Text>
        <Pressable onPress={onCopy} style={[styles.copyBtn, { backgroundColor: theme.card }]} hitSlop={8}>
          <Text style={[styles.copyTxt, { color: theme.text2 }]}>{copied ? t('copied') : t('copy')}</Text>
        </Pressable>
      </View>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.codeBody}>
        <Text selectable style={[styles.codeText, { color: theme.text, fontFamily: MONO_FONT }]}>
          {code}
        </Text>
      </ScrollView>
    </View>
  );
}

function renderInline(line: string, theme: Theme, baseColor: string, keyPrefix: string) {
  const tokenRe = /(`[^`\n]+`|\*\*[^*\n]+\*\*|\[[^\]\n]+\]\(https?:\/\/[^)\s]+\)|https?:\/\/[^\s<)]+)/g;
  const parts: React.ReactNode[] = [];
  let last = 0;
  let match: RegExpExecArray | null;
  let idx = 0;

  while ((match = tokenRe.exec(line)) !== null) {
    if (match.index > last) {
      parts.push(line.slice(last, match.index));
    }
    const tok = match[0];
    const k = `${keyPrefix}-${idx++}`;
    if (tok.startsWith('`') && tok.endsWith('`')) {
      parts.push(
        <Text key={k} style={{ fontFamily: MONO_FONT, fontSize: 15, backgroundColor: theme.s2, color: baseColor }}>
          {` ${tok.slice(1, -1)} `}
        </Text>
      );
    } else if (tok.startsWith('**') && tok.endsWith('**')) {
      parts.push(
        <Text key={k} style={{ fontWeight: '700', color: baseColor }}>
          {tok.slice(2, -2)}
        </Text>
      );
    } else if (tok.startsWith('[')) {
      const m = tok.match(/^\[([^\]\n]+)\]\((https?:\/\/[^)\s]+)\)$/);
      if (m) {
        const [, label, href] = m;
        parts.push(
          <Text key={k} style={{ color: theme.accent, textDecorationLine: 'underline' }} onPress={() => Linking.openURL(href)}>
            {label}
          </Text>
        );
      } else {
        parts.push(tok);
      }
    } else {
      parts.push(
        <Text key={k} style={{ color: theme.accent, textDecorationLine: 'underline' }} onPress={() => Linking.openURL(tok)}>
          {tok}
        </Text>
      );
    }
    last = match.index + tok.length;
  }
  if (last < line.length) {
    parts.push(line.slice(last));
  }
  return parts;
}

export function MarkdownText({
  text,
  theme,
  color,
}: {
  text: string;
  theme: Theme;
  color?: string;
}) {
  const baseColor = color || theme.text;
  const blocks: React.ReactNode[] = [];
  const sections = String(text || '').split(/```/);

  sections.forEach((part, secIdx) => {
    if (secIdx % 2 === 1) {
      const nl = part.indexOf('\n');
      const lang = nl >= 0 ? part.slice(0, nl).trim() : '';
      const code = (nl >= 0 ? part.slice(nl + 1) : part).replace(/\n$/, '');
      blocks.push(<CodeCard key={`code-${secIdx}`} lang={lang} code={code} theme={theme} />);
      return;
    }

    let para: string[] = [];
    let listItems: { marker: string; text: string }[] = [];

    const flushPara = (tag: string) => {
      if (para.length === 0) return;
      blocks.push(
        <Text key={`p-${secIdx}-${tag}`} selectable style={[styles.para, { color: baseColor }]}>
          {para.map((ln, i) => (
            <React.Fragment key={i}>
              {i > 0 ? '\n' : null}
              {renderInline(ln, theme, baseColor, `${secIdx}-${tag}-${i}`)}
            </React.Fragment>
          ))}
        </Text>
      );
      para = [];
    };

    const flushList = (tag: string) => {
      if (listItems.length === 0) return;
      blocks.push(
        <View key={`l-${secIdx}-${tag}`} style={styles.list}>
          {listItems.map((it, i) => (
            <View key={i} style={styles.listRow}>
              <Text style={[styles.listBullet, { color: theme.mute }]}>{it.marker}</Text>
              <Text selectable style={[styles.para, styles.listBody, { color: baseColor }]}>
                {renderInline(it.text, theme, baseColor, `${secIdx}-${tag}-li-${i}`)}
              </Text>
            </View>
          ))}
        </View>
      );
      listItems = [];
    };

    part.split('\n').forEach((line, lineIdx) => {
      const key = `${secIdx}-${lineIdx}`;
      if (!line.trim()) {
        flushPara(key);
        flushList(key);
        return;
      }
      const hm = line.match(/^(#{1,4})\s+(.*)$/);
      if (hm) {
        flushPara(key);
        flushList(key);
        const level = hm[1].length;
        const size = level === 1 ? 19 : level === 2 ? 18 : 17;
        blocks.push(
          <Text key={`h-${key}`} selectable style={[styles.heading, { fontSize: size, color: baseColor }]}>
            {renderInline(hm[2], theme, baseColor, `h-${key}`)}
          </Text>
        );
        return;
      }
      if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
        flushPara(key);
        flushList(key);
        blocks.push(<View key={`hr-${key}`} style={[styles.hr, { backgroundColor: theme.s3 }]} />);
        return;
      }
      const qm = line.match(/^>\s?(.*)$/);
      if (qm) {
        flushPara(key);
        flushList(key);
        blocks.push(
          <View key={`q-${key}`} style={[styles.quote, { borderLeftColor: theme.s3 }]}>
            <Text selectable style={[styles.para, { color: theme.text2 }]}>
              {renderInline(qm[1], theme, theme.text2, `q-${key}`)}
            </Text>
          </View>
        );
        return;
      }
      const ulm = line.match(/^\s*[-*+]\s+(.*)$/);
      const olm = line.match(/^\s*(\d+)[.)]\s+(.*)$/);
      if (ulm || olm) {
        flushPara(key);
        listItems.push({
          marker: olm ? `${olm[1]}.` : '•',
          text: (ulm ? ulm[1] : olm![1 + 1]) || '',
        });
        return;
      }
      flushList(key);
      para.push(line);
    });
    flushPara('end');
    flushList('end');
  });

  return <View style={styles.root}>{blocks}</View>;
}

const styles = StyleSheet.create({
  root: {
    gap: 8,
  },
  para: {
    fontSize: 17,
    lineHeight: 25,
  },
  heading: {
    fontWeight: '700',
    lineHeight: 26,
    marginTop: 2,
  },
  list: {
    gap: 4,
  },
  listRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 8,
  },
  listBullet: {
    fontSize: 17,
    lineHeight: 25,
    minWidth: 14,
  },
  listBody: {
    flex: 1,
  },
  quote: {
    borderLeftWidth: 3,
    paddingLeft: 10,
  },
  hr: {
    height: 1,
    marginVertical: 4,
  },
  codeWrap: {
    borderRadius: 14,
    overflow: 'hidden',
  },
  codeHead: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  codeLang: {
    fontSize: 13,
    fontWeight: '600',
  },
  copyBtn: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 3,
  },
  copyTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
  codeBody: {
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  codeText: {
    fontSize: 13.5,
    lineHeight: 20,
  },
});
