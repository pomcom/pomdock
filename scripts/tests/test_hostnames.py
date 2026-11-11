"""Run the actual provisioning expressions without touching any guest or host."""
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]


class HostnameTests(unittest.TestCase):
    def test_provisioners_use_the_instance_name_as_a_valid_hostname(self):
        cases = [('lab-direct', 'lab-direct'), ('Lab_VPN.v2', 'lab-vpn-v2'),
                 ('-__-', 'vm'), ('a'*62+'_long', 'a'*62), ('A'*70, 'a'*63)]
        for relative, variable in [('kali-vm/kali-i3-setup.sh', 'VM_HOSTNAME'),
                                   ('vm-profiles/linux-cloud-setup.sh', 'hostname')]:
            source = (ROOT/relative).read_text()
            expression = re.search(r'^'+variable+r'=\$\(.*\)\n'+variable+r'=.*$', source, re.M)
            self.assertIsNotNone(expression, relative)
            for name, expected in cases:
                with self.subTest(provisioner=relative, name=name):
                    result = subprocess.run(['bash', '-euc', 'VM_NAME=$1\n'+expression[0]+
                                             '\nprintf "%s" "$'+variable+'"', 'hostname-test', name],
                                            capture_output=True, text=True, check=True)
                    self.assertEqual(result.stdout, expected)


if __name__ == '__main__':
    unittest.main()
