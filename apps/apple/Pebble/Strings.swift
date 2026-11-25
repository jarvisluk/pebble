// 文案存储在同目录 strings-zh.json / strings-en.json，避免 Swift 巨型字典字面量拖慢编译。
import Foundation

private func loadBundledStrings(_ resource: String) -> [String: String] {
  guard let url = Bundle.main.url(forResource: resource, withExtension: "json"),
        let data = try? Data(contentsOf: url),
        let map = try? JSONDecoder().decode([String: String].self, from: data) else {
    return [:]
  }
  return map
}

let zhStrings: [String: String] = loadBundledStrings("strings-zh")
let enStrings: [String: String] = loadBundledStrings("strings-en")
