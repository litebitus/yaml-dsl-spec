//! The machine's credentials a schema link is read with: the `.netrc` entry for
//! its host.

use std::path::PathBuf;
use std::sync::Mutex;

/// A host's login, where it came from, and how its owner renews it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HostLogin {
    /// Where the login came from, as a report names it.
    pub credential_origin: String,
    pub login_name: String,
    pub login_password: String,
    pub renewal_fix: String,
}

impl HostLogin {
    /// The `Authorization` header value the login is sent as.
    pub fn authorization(&self) -> String {
        format!(
            "Basic {}",
            base64(format!("{}:{}", self.login_name, self.login_password).as_bytes())
        )
    }
}

/// Where a link's credentials come from.
pub trait CredentialSource: Send + Sync {
    /// The login `.netrc` gives `host`.
    fn host_login(&self, host: &str) -> Option<HostLogin>;
    /// Reads the credentials again when next asked, after every one was refused.
    fn forget_credentials(&self);
}

/// The machine's `.netrc`, read when first needed.
#[derive(Default)]
pub struct MachineCredentials {
    netrc_text_read: Mutex<Option<Option<String>>>,
}

impl CredentialSource for MachineCredentials {
    fn host_login(&self, host: &str) -> Option<HostLogin> {
        let mut read = self
            .netrc_text_read
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let text = read.get_or_insert_with(netrc_text).as_deref()?;
        host_login_of(text, host)
    }

    fn forget_credentials(&self) {
        *self
            .netrc_text_read
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = None;
    }
}

/// The login the machine's `.netrc` gives `host`: its `machine` entry, else its
/// `default` entry; the file `NETRC` names, else the one in the user's home folder.
pub fn netrc_login(host: &str) -> Option<HostLogin> {
    host_login_of(&netrc_text()?, host)
}

fn netrc_text() -> Option<String> {
    let path = match std::env::var_os("NETRC") {
        Some(path) => PathBuf::from(path),
        None => PathBuf::from(std::env::var_os("HOME")?).join(".netrc"),
    };
    std::fs::read_to_string(path).ok()
}

fn host_login_of(text: &str, host: &str) -> Option<HostLogin> {
    let (login_name, login_password) = netrc_entry(text, host)?;
    Some(HostLogin {
        credential_origin: format!("the `.netrc` entry for {host}"),
        login_name,
        login_password,
        renewal_fix: format!("update the `.netrc` entry for {host}"),
    })
}

/// The login and password a `.netrc`'s text gives `host`: its `machine` entry,
/// else its `default` entry.
fn netrc_entry(text: &str, host: &str) -> Option<(String, String)> {
    let mut words = text.split_whitespace();
    let mut found: Option<(String, String)> = None;
    let mut default: Option<(String, String)> = None;
    let mut current: Option<(bool, bool)> = None;
    let mut entry = (String::new(), String::new());
    let mut finish = |current: Option<(bool, bool)>, entry: &mut (String, String)| {
        if let Some((is_host, is_default)) = current {
            let taken = std::mem::take(entry);
            if is_host && found.is_none() {
                found = Some(taken.clone());
            }
            if is_default && default.is_none() {
                default = Some(taken);
            }
        }
    };
    while let Some(word) = words.next() {
        match word {
            "machine" => {
                finish(current, &mut entry);
                current = Some((words.next() == Some(host), false));
            }
            "default" => {
                finish(current, &mut entry);
                current = Some((false, true));
            }
            "login" => entry.0 = words.next().unwrap_or_default().to_owned(),
            "password" => entry.1 = words.next().unwrap_or_default().to_owned(),
            "account" => {
                words.next();
            }
            "macdef" => {
                finish(current, &mut entry);
                current = None;
                words.next();
            }
            _ => {}
        }
    }
    finish(current, &mut entry);
    found.or(default)
}

fn base64(bytes: &[u8]) -> String {
    const ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut encoded = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let joined = chunk
            .iter()
            .enumerate()
            .fold(0u32, |joined, (index, byte)| {
                joined | u32::from(*byte) << (16 - 8 * index)
            });
        for position in 0..4 {
            if position <= chunk.len() {
                encoded.push(ALPHABET[(joined >> (18 - 6 * position) & 0x3f) as usize] as char);
            } else {
                encoded.push('=');
            }
        }
    }
    encoded
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_netrc_gives_a_host_its_machine_entry_else_the_default() {
        let text = concat!(
            "machine mock.example login mock-user password mock-secret\n",
            "machine api.mock.example\n  login mock-owner\n  password mock-token\n",
            "macdef init\n",
            "default login anyone account mock password any-secret\n",
        );
        assert_eq!(
            netrc_entry(text, "api.mock.example"),
            Some(("mock-owner".into(), "mock-token".into()))
        );
        assert_eq!(
            netrc_entry(text, "other.example"),
            Some(("anyone".into(), "any-secret".into()))
        );
        assert_eq!(
            netrc_entry("machine a.example login x password y", "b.example"),
            None
        );
        let login = host_login_of(text, "api.mock.example").unwrap();
        assert_eq!(
            login.credential_origin,
            "the `.netrc` entry for api.mock.example"
        );
        assert_eq!(
            login.renewal_fix,
            "update the `.netrc` entry for api.mock.example"
        );
    }

    #[test]
    fn a_login_is_sent_base64_encoded() {
        let login = HostLogin {
            credential_origin: "mock".into(),
            login_name: "mock-user".into(),
            login_password: "mock-secret".into(),
            renewal_fix: String::new(),
        };
        assert_eq!(login.authorization(), "Basic bW9jay11c2VyOm1vY2stc2VjcmV0");
        assert_eq!(base64(b"a"), "YQ==");
        assert_eq!(base64(b"ab"), "YWI=");
    }
}
